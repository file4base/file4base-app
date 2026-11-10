# The Script Debugger

Running a script one step at a time: **Tools > Script Debugger** (#51).

Before it, a script that misbehaved could only be investigated by reading it.

## It drives the execution, it does not imitate it

The debugger advances the very execution state machine a normal run advances
(`ScriptExecution`): the same program counter, the same `If` frames, the same
local variables, the same halt semantics — one step at a time. Stepping
therefore has the same effects, in the same order, as running the script. That
is the whole design constraint: **a debugger that pretended to run what the
runner will not take would be worse than none** (#94).

So:

- A **conditional** script is stepped through as it runs: the `If` evaluates
  its condition, the branch that is not taken is marked skipped, and the steps
  after `End If` run. Previously each step was run in isolation, which stopped
  the session at the first `If` on a script that ran perfectly well.
- A step the runner does not take **stops the script** and shows the runner's
  own message, with the step marked as the one that stopped it. A condition or
  a *Set Variable* value File4Base cannot evaluate is one of those, and says
  so rather than being guessed (#93).
- A step that fails on its own terms — *Go to Layout* naming a layout that is
  not there, *Set Field* naming a field the table does not have — stops it the
  same way, with the same message the button would have shown.
- A **disabled** step is skipped, exactly as running the script skips it, and
  is marked as skipped rather than silently passed over.
- *Exit Script* ends this script and returns to its caller. *Halt Script* ends
  **every** running script, callers included, which is what Claris defines it
  as doing (#95); the session says which step halted the run.
- The **local variables** are the run's own: what a *Set Variable* step put
  there is what the next step reads, and `ScriptDebugSession.localVariables`
  shows them as they stand.

## What it offers

| Control | What it does |
| --- | --- |
| **Step** | Runs the step the cursor is on and stops again. |
| **Continue** | Runs until a breakpoint, the end, or a step that stops the script. Continuing *from* a breakpoint does not stop on that same breakpoint. |
| **Stop** | Ends the session where it stands. |
| **Start over** | Back to the first step, keeping the breakpoints. |
| **Breakpoint** | Click the dot beside a step. A long script does not have to be stepped from the top. |

The step about to run is marked and named in the status line; each step shows
what happened to it — ran, skipped, stopped here — and its parameters.

The steps are shown in the order they run, whatever order the catalog returned
them in.

## Where it runs

A script acts on the records on screen, so the debugger needs a layout open in
Browse mode: it drives the data browser, exactly as a layout button does. Open
without one, it says so instead of offering buttons that would do nothing.

## What is not here yet

- **No stepping into a sub-script.** A *Perform Script* step runs the whole
  sub-script as one step, which is what the runner does. Stepping into it
  needs a call stack in the session.
- **No variables to show**, because no step sets one. The Data Viewer's
  Variables tab says the same thing
  ([#50](https://github.com/file4base/file4base-app/issues/50)); when the
  runner takes *Set Variable*, both places show what a script holds.
- **No server-side runner.** Everything here runs in the client. If the
  server-side runner of the roadmap is built, the debugger drives that
  instead; the session does not assume either.
- **No editing while stopped.** The debugger runs a script; the Script
  Workspace changes it.
