const isNumber = require('is-number');
module.exports = (a, b) => {
  if (!isNumber(a) || !isNumber(b)) throw new TypeError('Both operands must be numbers');
  return Number(a) + Number(b);
};
