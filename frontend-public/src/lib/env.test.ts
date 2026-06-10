import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import {
  isPanelContext,
  isPublicContext,
  getPanelPath,
  getPublicPath,
  resolvePanelPath,
  resolvePublicPath,
} from './env';

describe('env helpers', () => {
  const originalWindow = globalThis.window;

  beforeEach(() => {
    // Reset window object before each test
    // @ts-ignore
    delete globalThis.window;
  });

  afterEach(() => {
    // Restore original window object after all tests
    globalThis.window = originalWindow;
  });

  describe('isPanelContext', () => {
    it('returns true when window is undefined (e.g. Server-Side Rendering or node env default)', () => {
      expect(isPanelContext()).toBe(true);
    });

    it('returns true when window.__APP_ENV__ is undefined (e.g. Dev mode / local next server)', () => {
      // @ts-ignore
      globalThis.window = {};
      expect(isPanelContext()).toBe(true);
    });

    it('returns true when window.__APP_ENV__.PANEL_PATH is set', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PANEL_PATH: '/admin',
        },
      };
      expect(isPanelContext()).toBe(true);
    });

    it('returns false when window.__APP_ENV__ exists but PANEL_PATH is undefined', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PUBLIC_PATH: '/public',
        },
      };
      expect(isPanelContext()).toBe(false);
    });
  });

  describe('isPublicContext', () => {
    it('returns false when window is undefined', () => {
      expect(isPublicContext()).toBe(false);
    });

    it('returns false when window.__APP_ENV__ is undefined', () => {
      // @ts-ignore
      globalThis.window = {};
      expect(isPublicContext()).toBe(false);
    });

    it('returns true when PUBLIC_PATH is defined but PANEL_PATH is undefined', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PUBLIC_PATH: '/status',
        },
      };
      expect(isPublicContext()).toBe(true);
    });

    it('returns false if both PUBLIC_PATH and PANEL_PATH are defined', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PUBLIC_PATH: '/status',
          PANEL_PATH: '/admin',
        },
      };
      expect(isPublicContext()).toBe(false);
    });
  });

  describe('getPanelPath', () => {
    it('returns default "/" when window is undefined', () => {
      expect(getPanelPath()).toBe('/');
    });

    it('returns custom PANEL_PATH when set', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PANEL_PATH: '/admin-dashboard/',
        },
      };
      expect(getPanelPath()).toBe('/admin-dashboard/');
    });
  });

  describe('getPublicPath', () => {
    it('returns default "/dashboard" when window is undefined or PUBLIC_PATH is unset', () => {
      expect(getPublicPath()).toBe('/dashboard');

      // @ts-ignore
      globalThis.window = { __APP_ENV__: {} };
      expect(getPublicPath()).toBe('/dashboard');
    });

    it('returns default "/dashboard" when PUBLIC_PATH is set to "/"', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PUBLIC_PATH: '/',
        },
      };
      expect(getPublicPath()).toBe('/dashboard');
    });

    it('returns custom PUBLIC_PATH when set to something other than "/"', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PUBLIC_PATH: '/status-page',
        },
      };
      expect(getPublicPath()).toBe('/status-page');
    });
  });

  describe('resolvePanelPath', () => {
    it('resolves relative path correctly with default panel base', () => {
      expect(resolvePanelPath('login')).toBe('/login');
      expect(resolvePanelPath('/login')).toBe('/login');
    });

    it('resolves relative path correctly with custom panel base', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PANEL_PATH: '/admin/',
        },
      };
      expect(resolvePanelPath('login')).toBe('/admin/login');
      expect(resolvePanelPath('/login')).toBe('/admin/login');
    });
  });

  describe('resolvePublicPath', () => {
    it('resolves relative path correctly with default public base', () => {
      expect(resolvePublicPath('agents')).toBe('/dashboard/agents');
      expect(resolvePublicPath('/agents')).toBe('/dashboard/agents');
    });

    it('resolves relative path correctly with custom public base', () => {
      // @ts-ignore
      globalThis.window = {
        __APP_ENV__: {
          PUBLIC_PATH: '/status/',
        },
      };
      expect(resolvePublicPath('agents')).toBe('/status/agents');
      expect(resolvePublicPath('/agents')).toBe('/status/agents');
    });
  });
});
