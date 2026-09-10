import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api, APIError, setCSRF } from './api';

describe('browser API boundary', () => {
  beforeEach(() => {
    setCSRF('csrf-fixture');
    vi.restoreAllMocks();
  });

  it('sends session-bound CSRF on writes', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ trade: {} }), { status: 202, headers: { 'Content-Type': 'application/json' } }));
    await api.confirm('quote-1', 'request-1');
    const [, init] = fetchMock.mock.calls[0];
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('csrf-fixture');
    expect(init?.credentials).toBe('same-origin');
  });

  it('keeps the public correlation ID on safe errors', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: 'quote_expired', message: 'Refresh the quote.', requestId: 'req_1' }), { status: 409, headers: { 'Content-Type': 'application/json' } }));
    await expect(api.confirm('quote-1', 'request-1')).rejects.toEqual(expect.objectContaining<Partial<APIError>>({ code: 'quote_expired', requestId: 'req_1' }));
  });
});
