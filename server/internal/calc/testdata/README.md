# Calculation conformance data

`conformance.json` records how FileMaker Pro answers a set of formulas, so the
engine can be checked against FileMaker rather than against what we believe
FileMaker does.

- **Formulas** are synthetic, written for this suite (`oracle-cases*.txt`).
  None comes from a customer solution.
- **Source:** FileMaker Pro 26.0.2, French (`fr-FR`), Windows, 24 November 2026.
  Formulas are therefore written with the decimal comma.
- **Method:** a calculation field in a new, empty file evaluates every case
  with `Evaluate`, and writes `EvaluationError ( Evaluate ( case ) )` and
  `Quote ( Evaluate ( case ) )` for each. The field's text was copied out and
  unquoted. `text` is the answer exactly as FileMaker writes it as text;
  `error` is FileMaker's error code, non-zero when the formula failed to
  parse or evaluate (the text is then irrelevant).
- **Notes:** a case with a `note` is one File4Base answers differently on
  purpose, or one that cannot be reproduced (random, the current year,
  state left by another case). `TestConformance` skips it.

To add cases, append formulas to a new `oracle-cases-N.txt`, evaluate them the
same way in FileMaker Pro and merge the answers; never edit an observed answer
by hand.
