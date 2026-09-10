import { describe, expect, it } from 'vitest';
import { displayDecimal } from './decimal';

describe('decimal display formatting', () => {
  it('expands scientific notation without losing decimal precision', () => {
    // Regression: ISSUE-006 — canonical decimal exponents leaked into account UI.
    // Found by /qa on 2026-09-10
    // Report: .gstack/qa-reports/qa-report-venue-trade-alert-net-2026-09-10.md
    expect(displayDecimal('1.0e3')).toBe('1000');
    expect(displayDecimal('1e-8')).toBe('0.00000001');
    expect(displayDecimal('-1.234567890123456789e4')).toBe('-12345.67890123456789');
    expect(displayDecimal('1.00063224e3')).toBe('1000.63224');
  });

  it('preserves plain canonical decimals and rejects unsafe expansion sizes', () => {
    expect(displayDecimal('99.99999924')).toBe('99.99999924');
    expect(displayDecimal('1e1001')).toBe('1e1001');
    expect(displayDecimal(undefined)).toBe('');
  });
});
