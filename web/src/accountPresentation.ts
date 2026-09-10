export function accountScopeLabel(
  accountType: string,
  liabilityStatus: string,
  hasDerivativePositions: boolean | null,
): string {
  const parts = [accountType.trim()].filter(Boolean);
  const normalizedLiability = liabilityStatus.trim().toLowerCase();
  if (normalizedLiability && normalizedLiability !== 'unknown') {
    parts.push(`liabilities ${normalizedLiability}`);
  }
  if (hasDerivativePositions !== null) {
    parts.push(`derivatives ${hasDerivativePositions ? 'present' : 'none'}`);
  }
  return parts.join(' · ');
}
