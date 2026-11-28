// Regenerates node-golden.json, the JavaScript behaviour the fmdapi package
// reproduces, from the Node.js FileMaker Server ships with (v24 or later):
//
//   node node-golden.js > node-golden.json
//
// Strings travel as arrays of UTF-16 code units so that lone surrogates
// survive the trip.
'use strict';

const units = (s) => Array.from({ length: s.length }, (_, i) => s.charCodeAt(i));

// body-parser's json() in strict mode, as FileMaker Server's front end runs it.
const FIRST_CHAR = /^[\x20\x09\x0a\x0d]*([^\x20\x09\x0a\x0d])/;
function bodyParse(body) {
  if (body.length === 0) return {};
  const m = FIRST_CHAR.exec(body);
  const first = m && m[1];
  if (first !== '{' && first !== '[') {
    const index = body.indexOf(first);
    let partial = '';
    if (index !== -1) {
      partial = body.substring(0, index) + '#';
      for (let i = index + 1; i < body.length; i++) partial += '#';
    }
    try {
      JSON.parse(partial);
      throw new SyntaxError('strict violation');
    } catch (e) {
      throw new SyntaxError(e.message.replace(/#+/g, (p) => body.substring(index, index + p.length)));
    }
  }
  return JSON.parse(body);
}

function run(fn, s) {
  try {
    return { in: units(s), ok: JSON.stringify(fn(s)) };
  } catch (e) {
    return { in: units(s), err: units(e.message) };
  }
}

const parseCases = [
  '{', '{"query":[{"ProductCode":"P001"}', '', ' ', 'x', 'abc', '[1,]', '{"a":1,}', '{"a" 1}', '{"a":}',
  '01', '-', '-a', '1.', '1.e', '1e', '1e+', 'tru', 'trux', 'tr1e', 'nul', '"abc', '"a\\x"', '"a\\u12g4"',
  '"a\u0001"', '{"a":1}x', '[1 2]', '{"a":1 "b":2}', 'NaN', 'undefined',
  '{"abcdefghijklmnopqrstuvwxyz": tru }', '{"abcdefghijklmnopqrstu":x}', '{\n"a":\n}', '{\r\n"a" 1}',
  '{\r"a"\r\r 1}', '{"é":"\\é"}', '{"😀":x}', '[]]', '{"a":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,]}',
  '{"a":1,"b":2,"c":3,"d":x}', '"\\', '"\\u12', '﻿{}', '{,}', '{"a":1}}', 'null', '[1,2', '{"a"',
  '{"a":', '{"a":1', '[', '"', '-0', '1e5x', '{"aaaaaaaaaaaaaaaaaaaa":1}  x', 'tx',
  '{"b":1,"a":2,"1":3,"0":4,"b":5}', '{"4294967294":1,"4294967295":2,"01":3,"1":4}',
  '{"s":"\\ud800","t":"\\ud83d\\ude00","u":"\\u2028\\u0000\\b\\f\\n\\r\\t\\"\\\\/"}',
  '[1.0,1e21,1e-7,-0,0.1,123456789012345678901234,5e-324,1.7976931348623157e308,1e400]',
  '{"a":{"b":[true,false,null,{}]}}', '[' + '['.repeat(20) + ']'.repeat(20) + ']',
  '"\ud800"', '{"x":"\udc00"}',
];
const bodyCases = parseCases.concat(['  x', '\n\t"a"', ' 1', 'true', '﻿[]', ' {"a":1}', '"str"', '#', '  ##x']);

const numberCases = [
  '0', '-0', '1', '-1', '1.5', '0.1', '0.000001', '0.0000001', '1e-7', '1.5e-10', '123456789012345680000',
  '1e21', '1.5e300', '-1.5e300', '5e-324', '1.7976931348623157e308', '9007199254740993', '1e100', '100',
  '29.9', '0.30000000000000004',
];

const stringCases = [
  'plain', 'quote"back\\slash/', '\u0000\u0001\u001f\u007f', '\b\f\n\r\t', '  ', 'é😀',
  '\ud800', '\udc00x', 'a\ud83d', '😀', '<script>&',
];

const parseIntCases = [
  '1', ' 42', '\t\n-7', '+3', '1.5', '1e3', '0x1F', '0X', '-0x10', '', ' ', 'abc', '12abc', '-0',
  '  5', '　 6', '﻿7', '᠎8', '99999999999999999999', '1000000000000000001',
  '0xFFFFFFFFFFFFFFFFFFFF', '00012', '٣', '-', '+-1',
];

const base64Cases = [
  'dXNlcjpwYXNz', 'dXNlcjpwYXNz=', 'dXNlcjpwYXN', 'dXNlcjpwYQ', 'dXNlcjpwY', 'dXN lcj\npwYXNz',
  'dXNl*cjpwYXNz', '-_-_', '+/+/', 'YQ==YWJj', '', '====', 'w6k6w6k=', 'Zm9vOg', 'OmJhcg',
];

const golden = {
  node: process.version,
  parse: parseCases.map((s) => run(JSON.parse, s)),
  body: bodyCases.map((s) => run(bodyParse, s)),
  numbers: numberCases.map((s) => ({ in: s, out: String(Number(s)) })),
  strings: stringCases.map((s) => ({ in: units(s), out: JSON.stringify(s) })),
  parseInt: parseIntCases.map((s) => ({ in: units(s), out: String(parseInt(s)) })),
  base64: base64Cases.map((s) => ({ in: s, out: Buffer.from(s, 'base64').toString('hex') })),
};
// One case per line.
const sections = Object.entries(golden).map(([k, v]) => JSON.stringify(k) + ': ' +
  (Array.isArray(v) ? '[\n  ' + v.map((c) => JSON.stringify(c)).join(',\n  ') + '\n]' : JSON.stringify(v)));
console.log('{\n' + sections.join(',\n') + '\n}');
