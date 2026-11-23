import { $t, getLocale } from '@/locales';

export function settlementCurrencyOptions() {
  return [
    { label: $t('business.us-dollar-usd'), value: 'USD' },
    { label: $t('business.chinese-yuan-cny'), value: 'CNY' },
    { label: $t('business.euro-eur'), value: 'EUR' },
    { label: $t('business.british-pound-gbp'), value: 'GBP' },
    { label: $t('business.japanese-yen-jpy'), value: 'JPY' },
    { label: $t('business.hong-kong-dollar-hkd'), value: 'HKD' },
    { label: $t('business.new-taiwan-dollar-twd'), value: 'TWD' },
    { label: $t('business.korean-won-krw'), value: 'KRW' }
  ];
}

export function currencyName(currency: string) {
  return settlementCurrencyOptions().find(item => item.value === currency)?.label ?? currency;
}

export function formatCurrency(value: number | string, currency = 'USD') {
  const amount = typeof value === 'string' ? Number(value) : value;
  if (!Number.isFinite(amount)) return '-';
  return new Intl.NumberFormat(getLocale(), {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 6
  }).format(amount);
}

export function formatCostUSD(value: number | string) {
  return formatCurrency(value, 'USD');
}

export function formatUnitAmount(value: number | null, unit: string) {
  if (value === null) return '-';
  const locale = getLocale();
  const normalized = unit.trim().toUpperCase();
  if (normalized === 'USD' || normalized === 'CNY') return formatCurrency(value, normalized);
  if (normalized === '%') return `${value.toFixed(1)}%`;
  if (normalized === 'CREDITS' || normalized === 'CREDIT' || normalized === '积分')
    return $t('business.creditsAmount', {
      value: value.toLocaleString(locale)
    });
  if (normalized === 'TOKENS' || normalized === 'TOKEN' || normalized === '令牌')
    return $t('business.tokensAmount', { value: value.toLocaleString(locale) });
  return `${value.toLocaleString(locale)} ${unit}`;
}

export function formatResetTime(value?: number) {
  if (!value) return '';
  const timestamp = value > 1e12 ? value : value * 1000;
  return new Date(timestamp).toLocaleString(getLocale(), {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  });
}
