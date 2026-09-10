import { flushPromises, mount } from '@vue/test-utils';
import Antd from 'ant-design-vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./api', async (importOriginal) => {
  const original = await importOriginal<typeof import('./api')>();
  return {
    ...original,
    setCSRF: vi.fn(),
    api: {
      login: vi.fn(),
      me: vi.fn(),
      logout: vi.fn(),
      venues: vi.fn(),
      account: vi.fn(),
      refreshAccount: vi.fn(),
      status: vi.fn(),
      trades: vi.fn(),
      quote: vi.fn(),
      confirm: vi.fn(),
      recheck: vi.fn(),
    },
  };
});

import App from './App.vue';
import { api, type AccountView, type QuoteView } from './api';

const mockedAPI = vi.mocked(api);

class FixtureWebSocket {
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  send() {}
  close() {}
}

const account: AccountView = {
  venue: 'bybit',
  environment: 'testnet',
  accountAlias: 'bybit-demo',
  accountType: 'UNIFIED',
  revision: 1,
  snapshotAsOf: '2026-09-10T20:00:00Z',
  completeness: 'complete',
  unpricedAssets: [],
  liabilityStatus: 'none',
  hasDerivativePositions: false,
  assets: [],
};

function authenticatedSession() {
  return {
    username: 'demo',
    csrfToken: 'csrf-fixture',
    expiresAt: '2026-09-10T21:00:00Z',
    readOnly: false,
  };
}

function mockBootstrap() {
  mockedAPI.venues.mockResolvedValue({
    venues: [{ id: 'bybit', environment: 'testnet', accountAlias: 'bybit-demo' }],
    defaultVenue: 'bybit',
    tradingEnabled: true,
  });
  mockedAPI.account.mockResolvedValue({ account });
  mockedAPI.status.mockResolvedValue({ venues: [], build: {} });
  mockedAPI.trades.mockResolvedValue({ trades: [] });
}

describe('native form submission regressions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    localStorage.clear();
    vi.stubGlobal('WebSocket', FixtureWebSocket);
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    );
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
    const getComputedStyle = window.getComputedStyle.bind(window);
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('submits login through the browser form and blocks missing fields', async () => {
    // Regression: ISSUE-001 — Ant Form swallowed native login submits without a model.
    // Found by /qa on 2026-09-10
    // Report: .gstack/qa-reports/qa-report-venue-trade-alert-net-2026-09-10.md
    mockedAPI.me.mockRejectedValueOnce(new Error('not authenticated'));
    mockedAPI.login.mockResolvedValue(authenticatedSession());
    mockBootstrap();

    const wrapper = mount(App, { attachTo: document.body, global: { plugins: [Antd] } });
    await flushPromises();

    const form = wrapper.find('form').element as HTMLFormElement;
    form.requestSubmit();
    await flushPromises();
    expect(mockedAPI.login).not.toHaveBeenCalled();

    await wrapper.find('input[autocomplete="username"]').setValue('demo-user');
    await wrapper.find('input[autocomplete="current-password"]').setValue('demo-password');
    form.requestSubmit();

    await vi.waitFor(() => {
      expect(mockedAPI.login).toHaveBeenCalledWith('demo-user', 'demo-password');
    });
    wrapper.unmount();
  });

  it('requests a quote through the native Quick Trade form submit', async () => {
    // Regression: ISSUE-001 — Ant Form swallowed native Quick Trade submits without a model.
    // Found by /qa on 2026-09-10
    // Report: .gstack/qa-reports/qa-report-venue-trade-alert-net-2026-09-10.md
    mockedAPI.me.mockResolvedValueOnce(authenticatedSession());
    mockBootstrap();
    const quote: QuoteView = {
      quoteId: 'quote-fixture',
      venue: 'bybit',
      routeId: 'bybit-usdt-btc',
      fromAsset: 'USDT',
      toAsset: 'BTC',
      spendBudget: '10',
      instrument: 'BTCUSDT',
      side: 'Buy',
      baseQty: '0.0001',
      limitPrice: '100500',
      timeInForce: 'IOC',
      referenceBid: '99900',
      referenceAsk: '100000',
      bookObservedAt: new Date().toISOString(),
      priceProtectionBps: 50,
      grossReceiveEstimate: '0.0001',
      netReceiveEstimate: '0.0000999',
      sourceDebitUpperBound: '10',
      createdAt: new Date().toISOString(),
      expiresAt: new Date(Date.now() + 5000).toISOString(),
      warnings: [],
      executable: true,
    };
    mockedAPI.quote.mockResolvedValue({ quote });

    const wrapper = mount(App, { attachTo: document.body, global: { plugins: [Antd] } });
    await flushPromises();
    const quickTrade = wrapper.findAll('button').find((button) => button.text().includes('Quick Trade'));
    await quickTrade!.trigger('click');
    await flushPromises();

    const amount = document.body.querySelector<HTMLInputElement>('input[placeholder="0.00"]')!;
    amount.value = '10';
    amount.dispatchEvent(new Event('input', { bubbles: true }));
    const form = amount.closest('form')!;
    form.requestSubmit();

    await vi.waitFor(() => {
      expect(mockedAPI.quote).toHaveBeenCalledWith('bybit', 'bybit-usdt-btc', '10');
    });
    wrapper.unmount();
  });
});
