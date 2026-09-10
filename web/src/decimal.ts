const SCIENTIFIC_DECIMAL = /^([+-]?)(\d+)(?:\.(\d*))?[eE]([+-]?\d+)$/;

/** Expands a decimal string for display without converting through IEEE-754. */
export function displayDecimal(value: string | undefined): string {
  const input = value?.trim() ?? '';
  const match = SCIENTIFIC_DECIMAL.exec(input);
  if (!match) return input;

  const exponent = Number(match[4]);
  if (!Number.isSafeInteger(exponent) || Math.abs(exponent) > 1_000) return input;

  const sign = match[1];
  const integer = match[2];
  const fraction = match[3] ?? '';
  const digits = integer + fraction;
  const decimalIndex = integer.length + exponent;

  let expanded: string;
  if (decimalIndex <= 0) {
    expanded = `0.${'0'.repeat(-decimalIndex)}${digits}`;
  } else if (decimalIndex >= digits.length) {
    expanded = `${digits}${'0'.repeat(decimalIndex - digits.length)}`;
  } else {
    expanded = `${digits.slice(0, decimalIndex)}.${digits.slice(decimalIndex)}`;
  }

  const [whole, decimal = ''] = expanded.split('.');
  const normalizedWhole = whole.replace(/^0+(?=\d)/, '') || '0';
  const normalizedDecimal = decimal.replace(/0+$/, '');
  const normalized = normalizedDecimal ? `${normalizedWhole}.${normalizedDecimal}` : normalizedWhole;
  return sign === '-' && normalized !== '0' ? `-${normalized}` : normalized;
}
