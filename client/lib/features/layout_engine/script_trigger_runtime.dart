/// Script triggers of layouts and of their field objects, run in Browse and
/// Find mode the way FileMaker runs them (#26).
///
/// A pre-event trigger (OnObjectExit, OnObjectValidate, OnRecordCommit,
/// OnRecordRevert, OnModeExit) runs before its event, and a script result
/// that evaluates to False — `Exit Script [Result: False]` — cancels the
/// event. OnObjectSave runs after a changed value is saved, and False keeps
/// the field active. Every other trigger runs after its event, and its
/// result is not read.
///
/// FileMaker runs a trigger again from the steps of its own script, which
/// can recurse without end. File4Base does not: a trigger is not run again
/// while its own script runs (the event goes ahead), and no more than
/// [ScriptTriggerRuntime.maxNesting] trigger scripts run inside one another.
library;

import 'dart:async';

import 'package:file4base_client/features/layout_engine/layout_action_runner.dart';
import 'package:file4base_client/features/layout_engine/models/layout_definition.dart';
import 'package:flutter/foundation.dart';

/// The layout events File4Base runs from the trigger list.
const kListTriggerEvents = ['OnRecordLoad', 'OnRecordCommit', 'OnRecordRevert', 'OnModeEnter', 'OnModeExit'];

/// The events of a field object's triggers File4Base runs.
const kObjectTriggerEvents = ['OnObjectEnter', 'OnObjectExit', 'OnObjectModify', 'OnObjectValidate', 'OnObjectSave'];

/// The layout events run from the layout's OnLayoutEnter / OnLayoutExit slots.
const kSlotTriggerEvents = ['OnLayoutEnter', 'OnLayoutExit'];

/// The events File4Base also runs in Find mode, for a trigger FileMaker runs
/// there. Records are neither loaded, committed nor reverted in Find mode,
/// and a find request's criteria are not validated nor saved.
const kFindModeTriggerEvents = ['OnObjectEnter', 'OnObjectExit', 'OnObjectModify', 'OnModeEnter', 'OnModeExit'];

bool _hasEvent(List<String> events, String event) {
  final e = event.trim().toLowerCase();
  return events.any((x) => x.toLowerCase() == e);
}

/// Whether File4Base runs a trigger of [event], set on a field object or on
/// the layout itself.
bool triggerEventRuns(String event, {required bool onObject}) => onObject
    ? _hasEvent(kObjectTriggerEvents, event)
    : _hasEvent(kListTriggerEvents, event) || _hasEvent(kSlotTriggerEvents, event);

/// Whether File4Base runs triggers of [event] in Find mode too.
bool triggerEventRunsInFind(String event) => _hasEvent(kFindModeTriggerEvents, event);

/// When a trigger of [event] runs, in words, for a developer deciding to
/// enable it.
String describeTriggerEvent(String event) => switch (event.trim().toLowerCase()) {
      'onlayoutenter' => 'when the layout is shown',
      'onlayoutexit' => 'when the layout is left for another one',
      'onrecordload' => 'after a record becomes the current one',
      'onrecordcommit' => 'before a changed record is committed; a result of False keeps it uncommitted',
      'onrecordrevert' => 'before a changed record is reverted; a result of False keeps the changes',
      'onmodeenter' => 'after the window enters a mode',
      'onmodeexit' => 'before the window leaves a mode; a result of False stays in it',
      'onobjectenter' => 'after the field is entered',
      'onobjectexit' => 'before the field is left; a result of False keeps it active',
      'onobjectmodify' => "after the field's content changes",
      'onobjectvalidate' => 'before a changed value is validated and saved; a result of False keeps it unsaved',
      'onobjectsave' => 'after a changed value is saved; a result of False keeps the field active',
      _ => 'on its event',
    };

/// Whether a trigger script's result cancels its event: a result that
/// evaluates to False as FileMaker reads a Boolean — zero, or text with no
/// number in it. No result, or an empty one, lets the event go ahead.
bool scriptResultCancels(String? result) {
  if (result == null) return false;
  final text = result.trim();
  if (text.isEmpty) return false;
  final number = num.tryParse(text);
  if (number != null) return number == 0;
  // FileMaker reads the number in a text: "a1" is 1, "abc" is nothing.
  final digits = RegExp(r'-?\d+(\.\d+)?').firstMatch(text);
  if (digits == null) return true;
  return (num.tryParse(digits.group(0)!) ?? 0) == 0;
}

/// A field box a user or a script can enter: a field object of the layout,
/// one repetition of it, or one cell of a portal row.
class TriggerBox {
  TriggerBox({
    required this.object,
    required this.key,
    required this.modified,
    required this.save,
    required this.focus,
    required this.hasFocus,
    this.portal,
    this.rowNumber = 0,
    this.rowId,
    this.find = false,
    this.stage,
  });

  /// The field object, and the record key of the box (the field, or
  /// "field[n]" for a repetition).
  final LayoutObjectModel object;
  final String key;

  /// The portal the box is in, its 1-based row and the related record's id.
  final LayoutObjectModel? portal;
  final int rowNumber;
  final String? rowId;

  /// A Find mode box: its text is a criterion, never saved.
  final bool find;

  /// Whether the box holds text not saved yet.
  final bool Function() modified;

  /// Puts the text typed in the box into the record while OnObjectValidate
  /// runs, as FileMaker shows the script the value being validated, and
  /// answers what puts the record back as it was.
  final VoidCallback Function()? stage;

  /// Validates and saves the box's text; false when it was refused.
  final Future<bool> Function() save;

  /// Puts the focus back into the box.
  final void Function() focus;
  final bool Function() hasFocus;

  /// The same box: the same object, repetition and portal row.
  bool same(TriggerBox? other) =>
      other != null && other.object.id == object.id && other.key == key && other.rowId == rowId && other.find == find;
}

/// Runs the triggers of a window, with the recursion guard.
class ScriptTriggerRuntime {
  ScriptTriggerRuntime({required this.perform, this.onStopped});

  /// Runs a trigger's script; the window's action runner.
  final Future<LayoutActionResult> Function(ButtonActionModel trigger) perform;

  /// Told why a trigger script stopped before its end.
  final void Function(String message)? onStopped;

  /// Trigger scripts running inside one another, at most.
  static const maxNesting = 8;

  // The triggers whose scripts the code running now was called from,
  // outermost first: a zone value, so that a trigger set off by its own
  // script is told from one set off by something else meanwhile.
  static final Object _chainKey = Object();

  List<String> get _chain => (Zone.current[_chainKey] as List<String>?) ?? const [];

  // The triggers running now, each with the answer it will give.
  final Map<String, Future<bool>> _inFlight = {};

  /// Trigger scripts running inside one another where this is called from.
  int get nesting => _chain.length;

  static String _keyOf(ButtonActionModel trigger) =>
      trigger.triggerId ?? '${trigger.objectId ?? ''}\u0000${trigger.event.toLowerCase()}';

  /// Runs [trigger], if any, and answers whether its event goes ahead.
  ///
  /// A trigger set off again by its own script, or nested too deep, does
  /// not run, and its event goes ahead. One set off by something else while
  /// its script runs (a second commit while OnRecordCommit decides) runs no
  /// second script: it waits, and gives the answer of the one running. A
  /// script that stops on a step it cannot run, or is halted, lets its
  /// event go ahead.
  Future<bool> fire(ButtonActionModel? trigger) => _fire(trigger, waitForRunning: true);

  Future<bool> _fire(ButtonActionModel? trigger, {required bool waitForRunning}) {
    if (trigger == null) return Future.value(true);
    final key = _keyOf(trigger);
    final chain = _chain;
    if (chain.contains(key) || chain.length >= maxNesting) return Future.value(true);
    final running = _inFlight[key];
    if (running != null) return waitForRunning ? running : Future.value(true);
    final answer = runZoned(() => _run(trigger), zoneValues: {
      _chainKey: List<String>.unmodifiable([...chain, key]),
    });
    _inFlight[key] = answer;
    return answer.whenComplete(() {
      if (identical(_inFlight[key], answer)) _inFlight.remove(key);
    });
  }

  Future<bool> _run(ButtonActionModel trigger) async {
    final result = await perform(trigger);
    if (!result.completed) {
      if (result.message != null) onStopped?.call(result.message!);
      return true;
    }
    if (result.halted) return true;
    return !scriptResultCancels(result.scriptResult);
  }

  // Post-event triggers started by a script step are waited for before the
  // script's next step, as FileMaker runs them before carrying on.
  int _sequence = 0;
  final Map<int, Future<void>> _pending = {};

  /// A mark to [settle] from.
  int mark() => _sequence;

  // Post-event triggers set off together run one after the other, in the
  // order of their events. One a trigger script sets off waits only for the
  // others of its own level, never for the script that set it off.
  final Map<int, Future<void>> _queues = {};

  /// Runs [trigger] as a post-event trigger, after those set off before it:
  /// its result is not read, and [settle] waits for it.
  void post(ButtonActionModel? trigger) {
    if (trigger == null) return;
    final level = nesting;
    // A post-event trigger already running is not run a second time.
    final run = (_queues[level] ?? Future<void>.value())
        .then((_) => _fire(trigger, waitForRunning: false))
        .then<void>((_) {}, onError: (Object _) {});
    _queues[level] = run;
    track(run);
  }

  /// Tracks [work] so that [settle] waits for it.
  void track(Future<void> work) {
    final id = ++_sequence;
    final done = work.then<void>((_) {}, onError: (Object _) {});
    _pending[id] = done;
    unawaited(done.whenComplete(() => _pending.remove(id)));
  }

  /// Waits for the triggers started since [since], and for those they
  /// started in turn.
  Future<void> settle(int since) async {
    while (true) {
      final later = [
        for (final e in _pending.entries)
          if (e.key > since) e.value,
      ];
      if (later.isEmpty) return;
      await Future.wait(later);
    }
  }
}
