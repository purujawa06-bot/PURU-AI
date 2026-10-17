'use strict';
// Release tags are case sensitive on GitHub, so both `v1.2.3` and `V1.2.3`
// releases must stay installable. These tests cover the tag resolution used by
// the npm postinstall download.
const test = require('node:test');
const assert = require('node:assert');
const { resolveTag, tagVariants } = require('./install');

function withVersion(value, fn) {
  const previous = process.env.PURU_AI_VERSION;
  try {
    if (value === undefined) delete process.env.PURU_AI_VERSION;
    else process.env.PURU_AI_VERSION = value;
    fn();
  } finally {
    if (previous === undefined) delete process.env.PURU_AI_VERSION;
    else process.env.PURU_AI_VERSION = previous;
  }
}

test('resolveTag keeps an explicit prefix and adds a missing v', () => {
  withVersion('V1.2.3', () => assert.equal(resolveTag(), 'V1.2.3'));
  withVersion('v1.2.3', () => assert.equal(resolveTag(), 'v1.2.3'));
  withVersion('1.2.3', () => assert.equal(resolveTag(), 'v1.2.3'));
  withVersion(undefined, () => assert.match(resolveTag(), /^v?\d+\.\d+\.\d+$/));
});

test('tagVariants also tries the other prefix spelling', () => {
  assert.deepStrictEqual(tagVariants('v1.2.3'), ['v1.2.3', 'V1.2.3']);
  assert.deepStrictEqual(tagVariants('V1.2.3'), ['V1.2.3', 'v1.2.3']);
  assert.deepStrictEqual(tagVariants('latest'), ['latest']);
});
