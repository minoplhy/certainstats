import { describe, it, expect } from 'vitest';
import { fmtBytes, fmtBps, fmtUptime, getUsageColor, isUnauthorized } from './utils';

describe('fmtBytes', () => {
  it('returns "-" for 0 or falsy inputs', () => {
    expect(fmtBytes(0)).toBe('-');
    // @ts-ignore
    expect(fmtBytes(null)).toBe('-');
  });

  it('formats bytes correctly into appropriate units', () => {
    expect(fmtBytes(512)).toBe('512.0 B');
    expect(fmtBytes(1024)).toBe('1.0 KB');
    expect(fmtBytes(1024 * 1024)).toBe('1.0 MB');
    expect(fmtBytes(1.5 * 1024 * 1024 * 1024)).toBe('1.5 GB');
    expect(fmtBytes(1024 * 1024 * 1024 * 1024)).toBe('1.0 TB');
  });
});

describe('fmtBps', () => {
  it('formats bits per second correctly', () => {
    expect(fmtBps(0)).toBe('0.0 B/s');
    expect(fmtBps(500)).toBe('500.0 B/s');
    expect(fmtBps(1024)).toBe('1.0 KB/s');
    expect(fmtBps(2.5 * 1024 * 1024)).toBe('2.5 MB/s');
  });
});

describe('fmtUptime', () => {
  it('returns "-" for 0 or falsy inputs', () => {
    expect(fmtUptime(0)).toBe('-');
  });

  it('formats uptime correctly', () => {
    // Under 1 hour -> show minutes
    expect(fmtUptime(45 * 60)).toBe('45m');
    // Under 1 day -> show hours and minutes
    expect(fmtUptime(3 * 3600 + 15 * 60)).toBe('3h 15m');
    // Over 1 day -> show days and hours
    expect(fmtUptime(2 * 86400 + 5 * 3600 + 30 * 60)).toBe('2d 5h');
  });
});

describe('getUsageColor', () => {
  it('returns status-online for low usage (< 50%)', () => {
    expect(getUsageColor(0)).toBe('var(--status-online)');
    expect(getUsageColor(49.9)).toBe('var(--status-online)');
  });

  it('returns yellow for moderate usage (50% <= usage < 85%)', () => {
    expect(getUsageColor(50)).toBe('#eab308');
    expect(getUsageColor(84.9)).toBe('#eab308');
  });

  it('returns status-offline for high usage (>= 85%)', () => {
    expect(getUsageColor(85)).toBe('var(--status-offline)');
    expect(getUsageColor(99)).toBe('var(--status-offline)');
  });
});

describe('isUnauthorized', () => {
  it('returns false for nil or generic errors', () => {
    expect(isUnauthorized(null)).toBe(false);
    expect(isUnauthorized(undefined)).toBe(false);
    expect(isUnauthorized(new Error('something failed'))).toBe(false);
  });

  it('returns true when status is 401', () => {
    expect(isUnauthorized({ status: 401 })).toBe(true);
    expect(isUnauthorized({ status: 400 })).toBe(false);
  });

  it('returns true when error message contains unauthorized keyword', () => {
    expect(isUnauthorized(new Error('Request failed with status code 401'))).toBe(true);
    expect(isUnauthorized(new Error('Unauthorized access'))).toBe(true);
    expect(isUnauthorized(new Error('unauthenticated user'))).toBe(true);
  });
});
