package fix

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/herefindalex/venuewire/internal/domain"
	"github.com/herefindalex/venuewire/internal/orderstate"
)

type NewOrderRequest struct{ Symbol, Side, OrderType, Qty, Price, ClOrdID, TimeInForce string }
type CancelRequest struct{ Symbol, OrderID, ClOrdID, ReqID string }
type AmendRequest struct{ Symbol, OrderID, ClOrdID, Qty, Price, ReqID string }
type pendingRequest struct{ OrderID, ClOrdID, ReqID string }

type OrderRouter struct {
	Session       *Session
	State         *orderstate.Service
	Now           func() time.Time
	RecvWindow    int64
	mu            sync.Mutex
	pendingCancel []pendingRequest
	pendingAmend  []pendingRequest
}

func (r *OrderRouter) Place(ctx context.Context, request NewOrderRequest) error {
	if r.Session == nil || r.State == nil {
		return errors.New("FIX order router requires session and state")
	}
	if err := validateNewOrder(request); err != nil {
		return err
	}
	now := r.now().UTC()
	order := domain.Order{Exchange: "bybit", Category: "spot", Symbol: request.Symbol, OrderLinkID: request.ClOrdID, Side: domain.Side(request.Side), Type: domain.OrderType(request.OrderType), Qty: request.Qty, Price: request.Price, Status: domain.OrderStatusPendingSubmit, RawStatus: "FIX_PENDING_SUBMIT", CreatedAt: now, UpdatedAt: now}
	if err := r.State.ApplyOrder(ctx, order); err != nil {
		return err
	}
	fields := append(r.requestHeader(""), Field{30010, "spot"}, Field{55, request.Symbol}, Field{54, fixSide(request.Side)}, Field{40, fixOrderType(request.OrderType)}, Field{38, request.Qty})
	if request.Price != "" {
		fields = append(fields, Field{44, request.Price})
	}
	fields = append(fields, Field{11, request.ClOrdID})
	if request.TimeInForce != "" {
		fields = append(fields, Field{59, fixTimeInForce(request.TimeInForce)})
	}
	if err := r.Session.SendApplication("D", fields); err != nil {
		order.RawStatus = "FIX_SUBMISSION_UNCERTAIN"
		order.UpdatedAt = r.now().UTC()
		_ = r.State.ApplyOrder(context.WithoutCancel(ctx), order)
		return fmt.Errorf("FIX order outcome uncertain for ClOrdID %q; reconcile before retrying: %w", request.ClOrdID, err)
	}
	return nil
}

func (r *OrderRouter) Cancel(ctx context.Context, request CancelRequest) error {
	if request.Symbol == "" || (request.OrderID == "" && request.ClOrdID == "") {
		return errors.New("FIX cancel requires symbol and OrderID or ClOrdID")
	}
	request.ReqID = r.reqID(request.ReqID)
	if err := r.markPendingCancel(ctx, request.OrderID, request.ClOrdID); err != nil {
		return err
	}
	fields := append(r.requestHeader(request.ReqID), Field{30010, "spot"}, Field{55, request.Symbol})
	if request.OrderID != "" {
		fields = append(fields, Field{37, request.OrderID})
	}
	if request.ClOrdID != "" {
		fields = append(fields, Field{11, request.ClOrdID})
	}
	r.mu.Lock()
	r.pendingCancel = append(r.pendingCancel, pendingRequest{request.OrderID, request.ClOrdID, request.ReqID})
	r.mu.Unlock()
	return r.Session.SendApplication("F", fields)
}

func (r *OrderRouter) Amend(_ context.Context, request AmendRequest) error {
	if request.Symbol == "" || (request.OrderID == "" && request.ClOrdID == "") || (request.Qty == "" && request.Price == "") {
		return errors.New("FIX amend requires symbol, an order identifier, and qty or price")
	}
	request.ReqID = r.reqID(request.ReqID)
	fields := append(r.requestHeader(request.ReqID), Field{30010, "spot"}, Field{55, request.Symbol})
	if request.OrderID != "" {
		fields = append(fields, Field{37, request.OrderID})
	}
	if request.ClOrdID != "" {
		fields = append(fields, Field{11, request.ClOrdID})
	}
	if request.Qty != "" {
		fields = append(fields, Field{38, request.Qty})
	}
	if request.Price != "" {
		fields = append(fields, Field{44, request.Price})
	}
	r.mu.Lock()
	r.pendingAmend = append(r.pendingAmend, pendingRequest{request.OrderID, request.ClOrdID, request.ReqID})
	r.mu.Unlock()
	return r.Session.SendApplication("XAR", fields)
}

func (r *OrderRouter) HandleMessage(ctx context.Context, message Message) error {
	t, _ := message.Get(35)
	switch t {
	case "8":
		return r.handleExecutionReport(ctx, message)
	case "XCA":
		return r.handleCancelAck(ctx, message)
	case "XAA":
		return r.handleAmendAck(ctx, message)
	case "j", "3":
		return fmt.Errorf("FIX reject: %s", valueOr(message, 58, ""))
	default:
		return fmt.Errorf("unsupported FIX application MsgType %q", t)
	}
}

func (r *OrderRouter) handleExecutionReport(ctx context.Context, message Message) error {
	orderID, _ := message.Get(37)
	linkID, _ := message.Get(11)
	raw := valueOr(message, 39, "")
	order := domain.Order{Exchange: "bybit", Category: valueOr(message, 30010, "spot"), Symbol: valueOr(message, 55, ""), OrderID: orderID, OrderLinkID: linkID, Side: domain.Side(sideFromFIX(valueOr(message, 54, ""))), Type: domain.OrderType(typeFromFIX(valueOr(message, 40, ""))), Qty: valueOr(message, 38, ""), Price: valueOr(message, 44, ""), CumFilledQty: valueOr(message, 14, ""), AvgFillPrice: valueOr(message, 6, ""), Status: statusFromFIX(raw), RawStatus: raw, UpdatedAt: parseFIXTime(valueOr(message, 60, ""))}
	if order.Status == domain.OrderStatusPendingSubmit {
		if _, current, found := findRouterOrder(r.State.Snapshot(), orderID, linkID); found && current.Status != domain.OrderStatusPendingSubmit {
			order.Status = current.Status
		}
	}
	if err := r.State.ApplyOrder(ctx, order); err != nil {
		return err
	}
	for _, execution := range executionsFromReport(message, order) {
		if err := r.State.RecordExecution(ctx, execution); err != nil {
			return err
		}
	}
	return nil
}

func (r *OrderRouter) handleCancelAck(ctx context.Context, message Message) error {
	orderID, hasOrder := message.Get(37)
	linkID, hasLink := message.Get(11)
	execType, accepted := message.Get(150)
	pending := r.popPending(true)
	if !hasOrder {
		orderID = pending.OrderID
	}
	if !hasLink {
		linkID = pending.ClOrdID
	}
	if !accepted || execType != "6" {
		_, current, found := findRouterOrder(r.State.Snapshot(), orderID, linkID)
		if !found {
			return errors.New("uncorrelated FIX cancel rejection")
		}
		current.Status = domain.OrderStatusRejectedCancel
		current.RawStatus = "XCA_REJECTED:" + valueOr(message, 58, "")
		current.UpdatedAt = r.now().UTC()
		return r.State.ApplyOrder(ctx, current)
	}
	return nil
}
func (r *OrderRouter) handleAmendAck(ctx context.Context, message Message) error {
	_, accepted := message.Get(150)
	pending := r.popPending(false)
	if !accepted {
		_, current, found := findRouterOrder(r.State.Snapshot(), pending.OrderID, pending.ClOrdID)
		if !found {
			return fmt.Errorf("uncorrelated FIX amend rejection: %s", valueOr(message, 58, ""))
		}
		current.RawStatus = "XAA_REJECTED:" + valueOr(message, 58, "")
		current.UpdatedAt = r.now().UTC()
		return r.State.ApplyOrder(ctx, current)
	}
	return nil
}

func (r *OrderRouter) requestHeader(reqID string) []Field {
	fields := []Field{{30002, strconv.FormatInt(r.now().UTC().UnixMilli(), 10)}, {30001, strconv.FormatInt(r.recvWindow(), 10)}}
	if reqID != "" {
		fields = append(fields, Field{30006, reqID})
	}
	return fields
}
func (r *OrderRouter) reqID(v string) string {
	if v != "" {
		return v
	}
	return "req-" + strconv.FormatInt(r.now().UTC().UnixNano(), 36)
}
func (r *OrderRouter) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
func (r *OrderRouter) recvWindow() int64 {
	if r.RecvWindow > 0 {
		return r.RecvWindow
	}
	return 5000
}
func (r *OrderRouter) popPending(cancel bool) pendingRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := &r.pendingAmend
	if cancel {
		list = &r.pendingCancel
	}
	if len(*list) == 0 {
		return pendingRequest{}
	}
	v := (*list)[0]
	*list = (*list)[1:]
	return v
}
func (r *OrderRouter) markPendingCancel(ctx context.Context, orderID, linkID string) error {
	_, order, found := findRouterOrder(r.State.Snapshot(), orderID, linkID)
	if !found {
		return errors.New("cannot cancel unknown local order")
	}
	order.Status = domain.OrderStatusPendingCancel
	order.RawStatus = "FIX_PENDING_CANCEL"
	order.UpdatedAt = r.now().UTC()
	return r.State.ApplyOrder(ctx, order)
}
func validateNewOrder(r NewOrderRequest) error {
	if r.Symbol == "" || r.Qty == "" || r.ClOrdID == "" {
		return errors.New("FIX new order requires symbol, qty, and ClOrdID")
	}
	if len(r.ClOrdID) > 36 {
		return errors.New("FIX ClOrdID exceeds 36 characters")
	}
	if r.Side != "Buy" && r.Side != "Sell" {
		return errors.New("FIX side must be Buy or Sell")
	}
	if r.OrderType != "Market" && r.OrderType != "Limit" {
		return errors.New("FIX order type must be Market or Limit")
	}
	if r.OrderType == "Limit" && r.Price == "" {
		return errors.New("FIX limit order requires price")
	}
	return nil
}
func fixSide(v string) string {
	if v == "Buy" {
		return "1"
	}
	return "2"
}
func sideFromFIX(v string) string {
	if v == "1" {
		return "Buy"
	}
	if v == "2" {
		return "Sell"
	}
	return ""
}
func fixOrderType(v string) string {
	if v == "Market" {
		return "1"
	}
	return "2"
}
func typeFromFIX(v string) string {
	if v == "1" {
		return "Market"
	}
	if v == "2" {
		return "Limit"
	}
	return ""
}
func fixTimeInForce(v string) string {
	switch strings.ToUpper(v) {
	case "GTC":
		return "1"
	case "IOC":
		return "3"
	case "FOK":
		return "4"
	case "POSTONLY":
		return "P"
	default:
		return v
	}
}
func statusFromFIX(v string) domain.OrderStatus {
	switch v {
	case "A":
		return domain.OrderStatusPendingSubmit
	case "0":
		return domain.OrderStatusNew
	case "1":
		return domain.OrderStatusPartiallyFilled
	case "2":
		return domain.OrderStatusFilled
	case "4", "F", "D":
		return domain.OrderStatusCancelled
	case "6":
		return domain.OrderStatusPendingCancel
	case "8":
		return domain.OrderStatusRejected
	default:
		return domain.OrderStatusUnknown
	}
}
func valueOr(m Message, tag int, fallback string) string {
	if v, ok := m.Get(tag); ok {
		return v
	}
	return fallback
}
func parseFIXTime(v string) time.Time { p, _ := time.Parse("20060102-15:04:05.000", v); return p.UTC() }
func findRouterOrder(s orderstate.Snapshot, orderID, linkID string) (string, domain.Order, bool) {
	if linkID != "" {
		if o, ok := s.Orders[linkID]; ok {
			return linkID, o, true
		}
	}
	for k, o := range s.Orders {
		if orderID != "" && o.OrderID == orderID {
			return k, o, true
		}
	}
	return "", domain.Order{}, false
}
func executionsFromReport(m Message, o domain.Order) []domain.Execution {
	var result []domain.Execution
	var current *domain.Execution
	for _, f := range m.Fields {
		switch f.Tag {
		case 30051:
			result = append(result, domain.Execution{Exchange: "bybit", Category: o.Category, Symbol: o.Symbol, ExecutionID: f.Value, OrderID: o.OrderID, OrderLinkID: o.OrderLinkID, Side: o.Side, ReceivedAt: time.Now().UTC()})
			current = &result[len(result)-1]
		case 30052:
			if current != nil {
				current.Qty = f.Value
			}
		case 30053:
			if current != nil {
				current.Price = f.Value
			}
		case 30054:
			if current != nil {
				current.ExchangeTime = parseFIXTime(f.Value)
			}
		}
	}
	return result
}
