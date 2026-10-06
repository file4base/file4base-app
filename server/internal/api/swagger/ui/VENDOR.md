# Vendored Swagger UI

Third-party files, not covered by File4Base's GPL-3.0 license: Swagger UI **v5.33.0** (https://github.com/swagger-api/swagger-ui/tree/v5.33.0/dist), licensed under the Apache License 2.0 (`LICENSE`, `NOTICE`). The `*.LICENSE.txt` files list the licenses of the libraries bundled inside the minified scripts. All of them are embedded in the server binary and served next to the scripts.

When updating Swagger UI, copy `LICENSE`, `NOTICE` and the `dist/*.LICENSE.txt` files of the same release and update the checksums below (`shasum -a 256`).

```
62df541529080464a7660adc793eab7128c6193ce3be24ddc1e0e0a4a63edc2f  swagger-ui-bundle.js
5243d492e14505e0cab87ac8b0195d0e615943e651743b2b698450a46eb470be  swagger-ui-standalone-preset.js
1ac324f7dcd27e4b9386b4bd6421271ec147e922a22c05ba24b11515e9aa6321  swagger-ui.css
cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30  LICENSE
0d20d1adef18aee3f40dd258172155521ce702ac445cb5f7b7d60ed32dad2fb2  NOTICE
63818894e4b04cd0e3180d9cb20761e227a939121e7484f8e1d528227c756f89  swagger-ui-bundle.js.LICENSE.txt
000580e4e2255ea6ccde0c47f6d75e2f703a1931298fde58756793b67c0daed9  swagger-ui-standalone-preset.js.LICENSE.txt
```
