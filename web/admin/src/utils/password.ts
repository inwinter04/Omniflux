export const MIN_PASSWORD_LENGTH = 8;
export const MAX_PASSWORD_LENGTH = 128;

/**
 * Validate password policy: 8-128 characters, no leading/trailing whitespace,
 * and contains at least one letter and one number.
 */
export function validatePasswordPolicy(value: string): boolean {
  if (!value || value.trim() !== value) {
    return false;
  }
  const chars = Array.from(value);
  if (chars.length < MIN_PASSWORD_LENGTH || chars.length > MAX_PASSWORD_LENGTH) {
    return false;
  }
  const hasLetter = /\p{L}/u.test(value);
  const hasDigit = /\p{N}/u.test(value);
  return hasLetter && hasDigit;
}

export type PasswordStrength = 'weak' | 'medium' | 'strong';

export function getPasswordStrength(value: string): PasswordStrength {
  if (!validatePasswordPolicy(value)) {
    return 'weak';
  }
  const length = Array.from(value).length;
  const hasLower = /[a-z]/.test(value);
  const hasUpper = /[A-Z]/.test(value);
  const hasDigit = /\d/.test(value);
  const hasSymbol = /[^\p{L}\p{N}\s]/u.test(value);
  const classes = [hasLower, hasUpper, hasDigit, hasSymbol].filter(Boolean).length;

  if ((length >= 12 && classes >= 3) || length >= 16) {
    return 'strong';
  }
  if (length >= 10 || classes >= 3) {
    return 'medium';
  }
  return 'weak';
}

const PASSWORD_CLASSES = [
  'abcdefghjkmnpqrstuvwxyz',
  'ABCDEFGHJKLMNPQRSTUVWXYZ',
  '23456789'
] as const;

function randomInt(limit: number): number {
  const arr = new Uint32Array(1);
  const max = Math.floor(0xffffffff / limit) * limit;
  let value = 0;
  do {
    window.crypto.getRandomValues(arr);
    value = arr[0];
  } while (value >= max);
  return value % limit;
}

/** Generate a 16-char readable initial password using Web Crypto. */
export function generateInitialPassword(length = 16): string {
  const chars: string[] = [];
  for (const cls of PASSWORD_CLASSES) {
    chars.push(cls[randomInt(cls.length)]);
  }
  const alphabet = PASSWORD_CLASSES.join('');
  while (chars.length < length) {
    chars.push(alphabet[randomInt(alphabet.length)]);
  }
  for (let i = chars.length - 1; i > 0; i -= 1) {
    const j = randomInt(i + 1);
    const tmp = chars[i];
    chars[i] = chars[j];
    chars[j] = tmp;
  }
  return chars.join('');
}
