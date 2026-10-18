import { describe, expect, it } from 'vitest';

import { appendSubQuery, parseSubQueryParams, serializeSubQueryParams } from '@/lib/sub-query';

const raw = JSON.stringify([
  { key: 'os', value: 'Android' },
  { key: 'app', value: 'v2rayNG 1.0' },
]);

describe('appendSubQuery', () => {
  it('adds the parameters to a plain link, escaping like the server', () => {
    expect(appendSubQuery('https://h/sub/abc', raw)).toBe(
      'https://h/sub/abc?os=Android&app=v2rayNG+1.0',
    );
  });

  it('keeps an existing query and does not override a present key', () => {
    expect(appendSubQuery('https://h/sub/abc?os=iOS', raw)).toBe(
      'https://h/sub/abc?os=iOS&app=v2rayNG+1.0',
    );
  });

  it('leaves empty links and empty or malformed settings alone', () => {
    expect(appendSubQuery('', raw)).toBe('');
    expect(appendSubQuery('https://h/sub/abc', '')).toBe('https://h/sub/abc');
    expect(appendSubQuery('https://h/sub/abc', 'nope')).toBe('https://h/sub/abc');
  });
});

describe('parseSubQueryParams', () => {
  it('skips invalid, reserved and duplicate keys', () => {
    const list = parseSubQueryParams(
      JSON.stringify([
        { key: 'ok', value: '1' },
        { key: 'bad key', value: '2' },
        { key: 'html', value: '1' },
        { key: 'ok', value: '3' },
      ]),
    );
    expect(list).toEqual([{ key: 'ok', value: '1' }]);
  });

  it('serialize drops blank keys', () => {
    expect(serializeSubQueryParams([{ key: ' ', value: 'x' }])).toBe('');
    expect(serializeSubQueryParams([{ key: 'os', value: 'A' }])).toBe('[{"key":"os","value":"A"}]');
  });
});
