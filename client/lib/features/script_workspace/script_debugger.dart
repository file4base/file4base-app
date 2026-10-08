// #51 — stepping through a script: what is about to run, what each step did,
// and where it stopped.
//
// The debugger drives the same runner the layout buttons use
// (LayoutActionRunner), one step at a time, so what it shows is what running
// the script does. A step the runner refuses stops the session with the
// runner's own message: a debugger that pretended to run what the runner
// will not would be worse than none.

import '../../core/models/script_models.dart';
import '../layout_engine/layout_action_runner.dart';

/// What has happened to one step of the script being debugged.
enum DebugStepState {
  /// Not reached yet.
  pending,

  /// About to run.
  current,

  /// Ran, and the runner was happy.
  done,

  /// Left out: the step is disabled in the script.
  skipped,

  /// Stopped here, with [ScriptDebugSession.message] saying why.
  failed,
}

/// One step, as the debugger shows it.
class DebugStep {
  final ScriptStepModel step;
  DebugStepState state;
  bool breakpoint;

  DebugStep(this.step, {this.state = DebugStepState.pending, this.breakpoint = false});

  int get sequence => step.sequenceIdx;
  String get type => step.stepType;
  bool get enabled => step.isEnabled;

  /// The parameters on one line, for the list.
  String get summary {
    if (step.params.isEmpty) return '';
    return step.params.entries.map((e) => '${e.key}: ${e.value}').join(', ');
  }
}

/// A script being stepped through.
class ScriptDebugSession {
  final ScriptModel script;
  final LayoutActionRunner runner;
  final List<DebugStep> steps;

  ScriptDebugSession({required this.script, required this.runner})
      : steps = (List<ScriptStepModel>.from(script.steps)
              ..sort((a, b) => a.sequenceIdx.compareTo(b.sequenceIdx)))
            .map(DebugStep.new)
            .toList();

  /// Where the session is: the index into [steps] of the step about to run.
  int _index = 0;
  bool _finished = false;
  bool _running = false;
  String? _message;

  int get index => _index;
  bool get finished => _finished;
  bool get running => _running;

  /// Why the session stopped, or what it is waiting on. Null while it has
  /// nothing to say.
  String? get message => _message;

  DebugStep? get currentStep => _index >= 0 && _index < steps.length ? steps[_index] : null;

  /// True once anything has run, so the interface can offer to start over.
  bool get started => _index > 0 || _finished;

  void toggleBreakpoint(int sequence) {
    for (final step in steps) {
      if (step.sequence == sequence) step.breakpoint = !step.breakpoint;
    }
  }

  /// Puts the session back at the top without losing the breakpoints.
  void restart() {
    _index = 0;
    _finished = false;
    _message = null;
    for (final step in steps) {
      step.state = DebugStepState.pending;
    }
    _markCurrent();
  }

  /// Ends the session where it stands.
  void stop() {
    if (_finished) return;
    _finished = true;
    _message = 'Stopped at step ${currentStep?.sequence ?? _index + 1}.';
    for (final step in steps) {
      if (step.state == DebugStepState.current) step.state = DebugStepState.pending;
    }
  }

  /// Runs one step and stops again.
  Future<void> step() async {
    if (_finished || _running) return;
    _running = true;
    try {
      await _runOne();
    } finally {
      _running = false;
    }
  }

  /// Runs until a breakpoint, the end of the script, or a step the runner
  /// refuses.
  Future<void> continueRun() async {
    if (_finished || _running) return;
    _running = true;
    try {
      // The step the session sits on runs even when it carries a breakpoint:
      // Continue from a breakpoint must not stop on that same breakpoint.
      var first = true;
      while (!_finished) {
        final next = currentStep;
        if (next != null && next.breakpoint && !first) {
          _message = 'Paused at the breakpoint on step ${next.sequence}.';
          break;
        }
        first = false;
        await _runOne();
      }
    } finally {
      _running = false;
    }
  }

  /// Runs the step the session is on and moves to the next.
  Future<void> _runOne() async {
    final step = currentStep;
    if (step == null) {
      _finish('The script ran to the end.');
      return;
    }

    // A disabled step is left out, exactly as running the script does.
    if (!step.enabled) {
      step.state = DebugStepState.skipped;
      _advance();
      return;
    }
    if (step.type == 'halt_script' || step.type == 'exit_script') {
      step.state = DebugStepState.done;
      _finish('The script ended at step ${step.sequence} (${step.type}).');
      return;
    }

    final result = await runner.runStep(step.type, step.step.params, depth: 0);
    if (!result.completed) {
      step.state = DebugStepState.failed;
      _finished = true;
      _message = 'Step ${step.sequence} stopped the script: ${result.message}';
      return;
    }
    step.state = DebugStepState.done;
    _advance();
  }

  void _advance() {
    _index++;
    if (_index >= steps.length) {
      _finish('The script ran to the end.');
      return;
    }
    _message = null;
    _markCurrent();
  }

  void _finish(String why) {
    _finished = true;
    _message = why;
    for (final step in steps) {
      if (step.state == DebugStepState.current) step.state = DebugStepState.pending;
    }
  }

  void _markCurrent() {
    for (var i = 0; i < steps.length; i++) {
      if (steps[i].state == DebugStepState.pending || steps[i].state == DebugStepState.current) {
        steps[i].state = i == _index ? DebugStepState.current : DebugStepState.pending;
      }
    }
  }

  /// Marks the first step as the one about to run.
  void begin() => _markCurrent();
}
