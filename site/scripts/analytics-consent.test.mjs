import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

// Execute the shipped inline script, so regressions in the Astro component fail CI.
const component = readFileSync(new URL('../src/components/AnalyticsConsent.astro', import.meta.url), 'utf8');
const script = component.match(/<script\b[^>]*>([\s\S]*?)<\/script>/)?.[1];
assert.ok(script, 'consent component must contain its inline script');

function render({ stored = null, readFails = false, writeFails = false } = {}) {
  const card = { hidden: true };
  const scripts = [];
  const buttons = ['denied', 'granted'].map(value => ({
    dataset: { consent: value },
    addEventListener(event, callback) { assert.equal(event, 'click'); this.click = callback; }
  }));
  const window = { location: { hostname: 'amsonia.com', pathname: '/docs/' } };
  const storage = {
    getItem(key) {
      assert.equal(key, 'amsonia-analytics-consent');
      if (readFails) throw new Error('storage access denied');
      return stored;
    },
    setItem(key, value) {
      assert.equal(key, 'amsonia-analytics-consent');
      if (writeFails) throw new Error('storage quota exceeded');
      stored = value;
    }
  };
  runInNewContext(script, {
    gaId: 'G-TEST', analyticsDomains: ['willuny.com', 'amsonia.com'], window,
    localStorage: storage,
    document: {
      querySelector: () => card,
      querySelectorAll: () => buttons,
      addEventListener() {},
      createElement: () => ({}),
      head: { appendChild: element => scripts.push(element) }
    }
  });
  return { card, scripts, buttons, window, stored: () => stored };
}

test('blocked storage still presents a choice and honors rejection', () => {
  const page = render({ readFails: true, writeFails: true });
  assert.equal(page.card.hidden, false);
  page.buttons[0].click();
  assert.equal(page.card.hidden, true);
  assert.equal(page.scripts.length, 0);
  assert.equal(page.window.dataLayer.at(-1)[2].analytics_storage, 'denied');
});

test('a failed write still honors consent for this page and loads analytics once', () => {
  const page = render({ writeFails: true });
  assert.equal(page.scripts.length, 0);
  page.buttons[1].click();
  page.buttons[1].click();
  assert.equal(page.card.hidden, true);
  assert.equal(page.scripts.length, 1);
  assert.equal(page.scripts[0].src, 'https://www.googletagmanager.com/gtag/js?id=G-TEST');
  assert.equal(page.window.dataLayer[0][2].analytics_storage, 'denied');
  assert.ok(page.window.dataLayer.some(entry => entry[0] === 'consent' && entry[1] === 'update' && entry[2].analytics_storage === 'granted'));
});

test('saved consent is honored even when storage becomes unwritable', () => {
  for (const stored of ['granted', 'denied']) {
    const page = render({ stored, writeFails: true });
    assert.equal(page.card.hidden, true);
    assert.equal(page.scripts.length, stored === 'granted' ? 1 : 0);
  }
});

test('new choices persist and unknown saved values require a fresh choice', () => {
  for (const value of ['granted', 'denied']) {
    const page = render({ stored: 'unexpected' });
    assert.equal(page.card.hidden, false);
    assert.equal(page.scripts.length, 0);
    page.buttons.find(button => button.dataset.consent === value).click();
    assert.equal(page.stored(), value);
    assert.equal(page.card.hidden, true);
  }
});
