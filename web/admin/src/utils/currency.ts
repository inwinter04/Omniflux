export const settlementCurrencyOptions = [
  { label: '美元（USD）', value: 'USD' },
  { label: '人民币（CNY）', value: 'CNY' },
  { label: '欧元（EUR）', value: 'EUR' },
  { label: '英镑（GBP）', value: 'GBP' },
  { label: '日元（JPY）', value: 'JPY' },
  { label: '港币（HKD）', value: 'HKD' },
  { label: '新台币（TWD）', value: 'TWD' },
  { label: '韩元（KRW）', value: 'KRW' }
];

export function currencyName(currency: string) {
  return settlementCurrencyOptions.find(item => item.value === currency)?.label ?? currency;
}

export function formatCurrency(value: number | string, currency = 'USD') {
  const amount = typeof value === 'string' ? Number(value) : value;
  if (!Number.isFinite(amount)) return '-';
  return new Intl.NumberFormat('zh-CN', {
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
  const normalized = unit.trim().toUpperCase();
  if (normalized === 'USD' || normalized === 'CNY') return formatCurrency(value, normalized);
  if (normalized === '%') return `${value.toFixed(1)}%`;
  if (normalized === 'CREDITS' || normalized === 'CREDIT' || normalized === '积分') return `${value.toLocaleString('zh-CN')} 积分`;
  if (normalized === 'TOKENS' || normalized === 'TOKEN' || normalized === '令牌') return `${value.toLocaleString('zh-CN')} 令牌`;
  return `${value.toLocaleString('zh-CN')} ${unit}`;
}

export function formatResetTime(value?: number) {
  if (!value) return '';
  const timestamp = value > 1e12 ? value : value * 1000;
  return new Date(timestamp).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false });
}
