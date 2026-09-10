export interface SessionView {
  username: string;
  csrfToken: string;
  expiresAt: string;
  readOnly: boolean;
}

export interface VenueView {
  id: 'bybit' | 'deribit';
  environment: 'testnet';
  accountAlias: string;
  accountSync?: string;
  readAvailable?: boolean;
  accountAvailable?: boolean;
  tradingAvailable?: boolean;
}

export interface AssetView {
  asset: string;
  balance: string;
  equity?: string;
  locked?: string;
  liability?: string;
  availableToTrade?: string;
  availableToTradeAsOf?: string;
  availableStatus: string;
  valuationQuantity?: string;
  quantityBasis: string;
  exchangeReportedUsdValue?: string;
  usdValue?: string;
  priceSource?: string;
  priceAsOf?: string;
  quality: string;
}

export interface AccountView {
  venue: string;
  environment: string;
  accountAlias: string;
  accountType: string;
  revision: number;
  snapshotAsOf: string;
  exchangeReportedTotalUsd?: string;
  exchangeReportedAsOf?: string;
  totalUsd?: string;
  pricedSubtotalUsd?: string;
  valuationBasis: string;
  completeness: string;
  unpricedAssets?: string[];
  liabilityStatus: string;
  hasDerivativePositions: boolean | null;
  derivativePositionEvidence?: string;
  assets: AssetView[];
}

export interface VenueStatus {
  orderRequestRttMs?: number;
  firstOrderEventLatencyMs?: number;
  firstExecutionEventLatencyMs?: number;
  orderRequestErrors: number;
  rateLimitState: string;
  reconciliationStatus: string;
  publicReceiveAgeMs?: number;
  privateReceiveAgeMs?: number;
  publicEventAgeMs?: number;
  privateEventAgeMs?: number;
  publicReconnects: number;
  privateReconnects: number;
  venue: string;
  rest: string;
  publicWs: string;
  privateWs: string;
  accountSync: string;
  accountAgeMs?: number;
  marketAgeMs?: number;
  reconnects: number;
  lastReconcileAt?: string;
  requestErrors: number;
  lastRequestRttMs?: number;
  reconciliationDiscrepancies: number;
}

export interface QuoteView {
  quoteId: string;
  venue: string;
  routeId: string;
  fromAsset: string;
  toAsset: string;
  spendBudget: string;
  instrument: string;
  side: string;
  baseQty: string;
  limitPrice: string;
  timeInForce: string;
  referenceBid: string;
  referenceAsk: string;
  bookObservedAt: string;
  priceProtectionBps: number;
  grossReceiveEstimate: string;
  netReceiveEstimate?: string;
  sourceDebitUpperBound: string;
  fees?: Array<{ asset: string; estimatedAmount: string; source: string }>;
  createdAt: string;
  expiresAt: string;
  warnings?: string[];
  executable: boolean;
  blockedReason?: string;
}

export interface TradeView {
  intentId: string;
  venue: string;
  routeId: string;
  fromAsset: string;
  toAsset: string;
  requestedAmount: string;
  status: string;
  resultStatus?: string;
  clientOrderId: string;
  venueOrderId?: string;
  filledBaseQty?: string;
  averagePrice?: string;
  fees?: Array<{ asset: string; amount: string; kind?: 'fee' | 'rebate' }>;
  netDestinationReceived?: string;
  fillDetailsStatus: string;
  feeDetailsStatus: string;
  balanceSyncStatus: string;
  createdAt: string;
  updatedAt: string;
  lastCheckedAt?: string;
  message?: string;
  lifecycle: Array<{ status: string; at: string; detail?: string }>;
}

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public requestId?: string,
  ) {
    super(message);
  }
}

let csrfToken = '';
export function setCSRF(token: string) {
  csrfToken = token;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body) headers.set('Content-Type', 'application/json');
  if (init.method && init.method !== 'GET' && csrfToken) headers.set('X-CSRF-Token', csrfToken);
  const response = await fetch(path, { ...init, headers, credentials: 'same-origin' });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new APIError(response.status, body.error ?? 'request_failed', body.message ?? 'The request failed.', body.requestId);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const api = {
  login: (username: string, password: string) => request<SessionView>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }),
  me: () => request<SessionView>('/api/auth/me'),
  logout: () => request<void>('/api/auth/logout', { method: 'POST' }),
  venues: () => request<{ venues: VenueView[]; defaultVenue: string; tradingEnabled: boolean }>('/api/venues'),
  account: (venue: string) => request<{ account: AccountView }>(`/api/venues/${venue}/account`),
  refreshAccount: (venue: string) => request<{ account: AccountView }>(`/api/venues/${venue}/account/refresh`, { method: 'POST' }),
  status: () => request<{ venues: VenueStatus[]; build: Record<string, string> }>('/api/system/status'),
  trades: () => request<{ trades: TradeView[] }>('/api/trades?limit=50'),
  quote: (venue: string, routeId: string, amount: string) => request<{ quote: QuoteView }>(`/api/venues/${venue}/quotes`, { method: 'POST', body: JSON.stringify({ routeId, amount }) }),
  confirm: (quoteId: string, clientRequestId: string) => request<{ trade: TradeView }>('/api/trades/confirm', { method: 'POST', body: JSON.stringify({ quoteId, clientRequestId }) }),
  recheck: (intentId: string) => request<{ trade: TradeView; recheckInProgress: boolean }>(`/api/trades/${intentId}/recheck`, { method: 'POST' }),
};
