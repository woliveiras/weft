const { test } = require('node:test');
const assert = require('node:assert/strict');
const sum = require('./built.cjs');
test('built module sums numbers and numeric input', () => {
  assert.equal(sum(2, 3), 5);
  assert.equal(sum('2', -3), -1);
});
test('built module rejects invalid operands', () => {
  assert.throws(() => sum('invalid', 1), TypeError);
});
