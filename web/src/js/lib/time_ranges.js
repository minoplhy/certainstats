// Quick ranges offered by the admin time picker and the public range bar.
export const TIME_RANGES = [
  { label: '1h', value: 1 },
  { label: '6h', value: 6 },
  { label: '12h', value: 12 },
  { label: '24h', value: 24 },
  { label: '2d', value: 48 },
  { label: '7d', value: 168 },
  { label: '30d', value: 720 },
  { label: '90d', value: 2160 },
  { label: '180d', value: 4320 },
  { label: '1y', value: 8760 },
  { label: '2y', value: 17520 }
];

// rangeLabel names a quick range in words: 1 → "1 hour", 168 → "7 days", 8760 → "1 year".
export function rangeLabel(hours) {
  const plural = (n, unit) => n + ' ' + unit + (n === 1 ? '' : 's');
  if (hours % 8760 === 0) return plural(hours / 8760, 'year');
  if (hours >= 48 && hours % 24 === 0) return plural(hours / 24, 'day');
  return plural(hours, 'hour');
}
