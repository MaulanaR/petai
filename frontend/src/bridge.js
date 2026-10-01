// Thin wrapper around the Wails bindings so the rest of the UI never touches window.go directly.
import * as Go from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

export const api = Go;

export function on(name, fn) {
  return EventsOn(name, fn);
}

/** Call a Go method, swallowing errors into a [result, error] tuple. */
export async function call(fn, ...args) {
  try {
    return [await fn(...args), null];
  } catch (e) {
    return [null, typeof e === 'string' ? e : e?.message || String(e)];
  }
}
