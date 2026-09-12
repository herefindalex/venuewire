import { expect, test, type Page, type Route } from '@playwright/test';
import path from 'node:path';

const now = '2026-09-12T14:30:00Z';

const accounts = {
  bybit: {
    venue: 'bybit',
    environment: 'testnet',
    accountAlias: 'public-demo',
    accountType: 'UNIFIED',
    revision: 12,
    snapshotAsOf: now,
    exchangeReportedTotalUsd: '12548.21',
    exchangeReportedAsOf: now,
    totalUsd: '12551.84',
    valuationBasis: 'public USD mark prices',
    completeness: 'complete',
    liabilityStatus: 'none',
    hasDerivativePositions: false,
    assets: [
      {
        asset: 'BTC',
        balance: '0.0824',
        equity: '0.0824',
        availableToTrade: '0.0824',
        availableStatus: 'verified',
        valuationQuantity: '0.0824',
        quantityBasis: 'wallet balance',
        exchangeReportedUsdValue: '6451.68',
        usdValue: '6455.31',
        priceSource: 'Deribit btc_usd index',
        quality: 'fresh',
      },
      {
        asset: 'ETH',
        balance: '1.5',
        equity: '1.5',
        availableToTrade: '1.5',
        availableStatus: 'verified',
        valuationQuantity: '1.5',
        quantityBasis: 'wallet balance',
        exchangeReportedUsdValue: '3896.53',
        usdValue: '3896.53',
        priceSource: 'Deribit eth_usd index',
        quality: 'fresh',
      },
      {
        asset: 'USDT',
        balance: '2200',
        equity: '2200',
        availableToTrade: '2200',
        availableStatus: 'verified',
        valuationQuantity: '2200',
        quantityBasis: 'wallet balance',
        exchangeReportedUsdValue: '2200',
        usdValue: '2200',
        priceSource: 'venue-reported USD value',
        quality: 'fresh',
      },
    ],
  },
  deribit: {
    venue: 'deribit',
    environment: 'testnet',
    accountAlias: 'public-demo',
    accountType: 'account summaries',
    revision: 8,
    snapshotAsOf: now,
    pricedSubtotalUsd: '9850.20',
    valuationBasis: 'public USD mark prices',
    completeness: 'partial',
    unpricedAssets: ['USDC'],
    liabilityStatus: 'unknown',
    hasDerivativePositions: null,
    derivativePositionEvidence: 'account summary does not prove derivative-position absence',
    assets: [
      {
        asset: 'BTC',
        balance: '0.1',
        equity: '0.1',
        availableToTrade: '0.1',
        availableStatus: 'verified',
        valuationQuantity: '0.1',
        quantityBasis: 'account summary balance',
        usdValue: '7830.20',
        priceSource: 'Deribit btc_usd index',
        quality: 'fresh',
      },
      {
        asset: 'USDC',
        balance: '2020',
        equity: '2020',
        availableToTrade: '2020',
        availableStatus: 'verified',
        valuationQuantity: '2020',
        quantityBasis: 'account summary balance',
        quality: 'unknown',
      },
    ],
  },
};

const liveStatuses = [
  {
    venue: 'bybit', rest: 'LIVE', publicWs: 'LIVE', privateWs: 'LIVE', accountSync: 'SYNCED',
    reconnects: 0, publicReconnects: 0, privateReconnects: 0, requestErrors: 0,
    orderRequestErrors: 0, rateLimitState: 'NORMAL', reconciliationStatus: 'SYNCED',
    reconciliationDiscrepancies: 0, publicReceiveAgeMs: 180, publicEventAgeMs: 240,
    privateReceiveAgeMs: 320, privateEventAgeMs: 1500, orderRequestRttMs: 84,
    firstOrderEventLatencyMs: 126, firstExecutionEventLatencyMs: 241,
  },
  {
    venue: 'deribit', rest: 'LIVE', publicWs: 'LIVE', privateWs: 'LIVE', accountSync: 'SYNCED',
    reconnects: 1, publicReconnects: 1, privateReconnects: 0, requestErrors: 0,
    orderRequestErrors: 0, rateLimitState: 'NORMAL', reconciliationStatus: 'SYNCED',
    reconciliationDiscrepancies: 0, publicReceiveAgeMs: 210, publicEventAgeMs: 310,
    privateReceiveAgeMs: 290, privateEventAgeMs: 2100, orderRequestRttMs: 91,
    firstOrderEventLatencyMs: 138, firstExecutionEventLatencyMs: 267,
  },
];

const filledTrade = {
  intentId: 'public-trade-001', venue: 'deribit', routeId: 'deribit-usdc-btc',
  fromAsset: 'USDC', toAsset: 'BTC', requestedAmount: '100', status: 'Filled',
  resultStatus: 'FILLED', clientOrderId: 'vw-public-001', venueOrderId: 'sanitized',
  filledBaseQty: '0.00127', averagePrice: '78350', fees: [{ asset: 'USDC', amount: '0.08' }],
  netDestinationReceived: '0.00127', fillDetailsStatus: 'COMPLETE', feeDetailsStatus: 'COMPLETE',
  balanceSyncStatus: 'SYNCED', createdAt: '2026-09-12T14:28:00Z', updatedAt: '2026-09-12T14:28:01Z',
  lifecycle: [
    { status: 'Submitted', at: '2026-09-12T14:28:00Z' },
    { status: 'PartiallyFilled', at: '2026-09-12T14:28:00.500Z' },
    { status: 'Filled', at: '2026-09-12T14:28:01Z' },
  ],
};

async function installMockWebSocket(page: Page) {
  await page.addInitScript(() => {
    class FixtureWebSocket {
      static readonly OPEN = 1;
      static readonly CLOSED = 3;
      readonly url: string;
      readyState = FixtureWebSocket.OPEN;
      onopen: ((event: Event) => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onclose: ((event: CloseEvent) => void) | null = null;

      constructor(url: string) {
        this.url = url;
        setTimeout(() => this.onopen?.(new Event('open')), 0);
      }

      send() {}

      close() {
        this.readyState = FixtureWebSocket.CLOSED;
      }
    }

    Object.defineProperty(window, 'WebSocket', { value: FixtureWebSocket });
  });
}

function fulfillJSON(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
}

async function installAuthenticatedAPI(page: Page, degraded = false) {
  await page.route('**/api/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const pathname = url.pathname;

    if (pathname === '/api/auth/me') {
      return fulfillJSON(route, { username: 'demo', csrfToken: 'sanitized-csrf', expiresAt: '2099-09-12T15:30:00Z', readOnly: false });
    }
    if (pathname === '/api/venues') {
      return fulfillJSON(route, {
        venues: [
          { id: 'bybit', environment: 'testnet', accountAlias: 'public-demo' },
          { id: 'deribit', environment: 'testnet', accountAlias: 'public-demo' },
        ],
        defaultVenue: 'bybit',
        tradingEnabled: true,
      });
    }
    if (pathname.includes('/account')) {
      const venue = pathname.includes('/deribit/') ? 'deribit' : 'bybit';
      return fulfillJSON(route, { account: accounts[venue] });
    }
    if (pathname === '/api/system/status') {
      const statuses = degraded
        ? [{ ...liveStatuses[0], rest: 'RECOVERING', publicWs: 'STALE', accountSync: 'STALE', rateLimitState: 'LIMITED' }]
        : liveStatuses;
      return fulfillJSON(route, { venues: statuses, build: { environment: 'Testnet', backend: 'Go', frontend: 'Vue 3' } });
    }
    if (pathname === '/api/trades' && request.method() === 'GET') {
      const trades = degraded
        ? [{ ...filledTrade, intentId: 'public-unknown-001', venue: 'bybit', status: 'Unknown', resultStatus: 'OUTCOME_UNKNOWN', balanceSyncStatus: 'PENDING' }]
        : [filledTrade];
      return fulfillJSON(route, { trades });
    }
    if (pathname.endsWith('/quotes')) {
      return fulfillJSON(route, {
        quote: {
          quoteId: 'public-quote-001', venue: 'deribit', routeId: 'deribit-usdc-btc',
          fromAsset: 'USDC', toAsset: 'BTC', spendBudget: '100', instrument: 'BTC_USDC',
          side: 'Buy', baseQty: '0.00127', limitPrice: '78741.75', timeInForce: 'IOC',
          referenceBid: '78280', referenceAsk: '78350', bookObservedAt: now,
          priceProtectionBps: 50, grossReceiveEstimate: '0.00127', netReceiveEstimate: '0.001269',
          sourceDebitUpperBound: '100', createdAt: new Date().toISOString(),
          expiresAt: new Date(Date.now() + 30000).toISOString(), warnings: [], executable: true,
        },
      });
    }
    if (pathname === '/api/trades/confirm') return fulfillJSON(route, { trade: filledTrade });
    if (pathname.endsWith('/recheck')) return fulfillJSON(route, { trade: filledTrade, recheckInProgress: false });
    if (pathname === '/api/auth/logout') return route.fulfill({ status: 204 });

    return fulfillJSON(route, { error: 'not_found', message: 'Fixture route missing.' }, 404);
  });
}

test('renders the sanitized public console and captures the README image @public-screenshot', async ({ page }) => {
  await installMockWebSocket(page);
  await installAuthenticatedAPI(page);
  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'Account overview' })).toBeVisible();
  await expect(page.getByText('TESTNET', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'System status' })).toBeVisible();
  await expect(page.getByText('public-demo')).toBeVisible();

  if (process.env.CAPTURE_PUBLIC_SCREENSHOT === '1') {
    await page.setViewportSize({ width: 1440, height: 1050 });
    await page.screenshot({ path: path.resolve('../docs/assets/venuewire-console.png'), fullPage: true });
  }
});

test('switches venue and completes the protected Testnet trade flow', async ({ page }) => {
  await installMockWebSocket(page);
  await installAuthenticatedAPI(page);
  await page.goto('/');

  await page.locator('.venue-select').click();
  await page.getByText('Deribit', { exact: true }).last().click();
  await expect(page.getByText('DERIBIT · TESTNET ACCOUNT')).toBeVisible();

  await page.getByRole('button', { name: 'Quick Trade' }).click();
  await page.getByPlaceholder('0.00').fill('100');
  await page.getByRole('button', { name: 'Review protected IOC' }).click();
  await expect(page.getByRole('dialog').getByText('USDC → BTC')).toBeVisible();
  await page.getByRole('button', { name: 'Confirm Testnet trade' }).click();

  await expect(page.getByText('FILLED', { exact: true })).toBeVisible();
  await expect(page.getByText('Balance SYNCED')).toBeVisible();
});

test('renders outcome unknown, stale market data, and venue recovery independently', async ({ page }) => {
  await installMockWebSocket(page);
  await installAuthenticatedAPI(page, true);
  await page.goto('/');

  await expect(page.getByText('Unknown', { exact: true })).toBeVisible();
  await expect(page.getByText('STALE', { exact: true }).first()).toBeVisible();
  await expect(page.getByText('RECOVERING', { exact: true })).toBeVisible();
  await expect(page.getByText('LIMITED', { exact: true })).toBeVisible();
});

test('returns an expired session to the sign-in boundary', async ({ page }) => {
  await installMockWebSocket(page);
  await page.route('**/api/auth/me', (route) => fulfillJSON(route, { error: 'session_expired', message: 'Session expired.' }, 401));
  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'VenueWire' })).toBeVisible();
  await expect(page.getByText('Testnet execution prototype. No real-money trading.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
});
