import { describe, expect, it } from 'vitest';
import { accountScopeLabel } from './accountPresentation';

describe('account scope presentation', () => {
  it('omits unknown scope details from the account heading', () => {
    // Regression: ISSUE-007 — Deribit rendered two unknown labels as headline noise.
    // Found by /qa on 2026-09-10
    // Report: .gstack/qa-reports/qa-report-venue-trade-alert-net-2026-09-10.md
    expect(accountScopeLabel('account summaries', 'unknown', null)).toBe('account summaries');
  });

  it('keeps scope details when the venue reports a definite state', () => {
    expect(accountScopeLabel('UNIFIED', 'none', false)).toBe('UNIFIED · liabilities none · derivatives none');
    expect(accountScopeLabel('UNIFIED', 'present', true)).toBe('UNIFIED · liabilities present · derivatives present');
  });
});
