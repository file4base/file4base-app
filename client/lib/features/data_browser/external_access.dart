// #107 — records of another solution's table, read through an external
// occurrence. The server reads them as the account this session is signed in
// with in that solution; this is what the client keeps about them for one
// session: the tables of those occurrences, and signing in to a solution
// that is not signed in to yet.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/auth/database_login_dialog.dart';
import 'package:flutter/material.dart';

/// The external occurrences of one session.
class ExternalAccess extends ChangeNotifier {
  ExternalAccess._(this.apiClient);

  /// The one for the session [apiClient] holds: clients of the same session
  /// (one per occurrence, for instance) share it.
  factory ExternalAccess.of(ApiClient apiClient) =>
      _bySession.putIfAbsent(apiClient.authToken ?? '', () => ExternalAccess._(apiClient));
  static final Map<String, ExternalAccess> _bySession = {};

  final ApiClient apiClient;

  /// The solution the session is signed in to, named when asking to sign in
  /// to one it reads.
  String solutionName = '';

  final Map<String, Future<TableModel>> _tables = {};
  final Map<String, Future<bool>> _signIns = {};
  final Set<String> _asked = {};

  /// The table of [occurrence] in the other solution. A failure is not kept,
  /// so the next call asks again.
  Future<TableModel> table(TableOccurrenceModel occurrence) =>
      _tables.putIfAbsent(occurrence.id, () => _readTable(occurrence.id));

  Future<TableModel> _readTable(String occurrence) async {
    try {
      return await apiClient.getOccurrenceTable(occurrence);
    } on Object {
      unawaited(_tables.remove(occurrence));
      rethrow;
    }
  }

  /// Asks for an account of the solution [problem] names and signs the
  /// session in to it. Every view that meets the same solution shares one
  /// dialog; once signed in, they read again.
  Future<bool> signIn(BuildContext context, ExternalUnavailable problem) {
    final database = problem.database;
    if (database.isEmpty) return Future.value(false);
    return _signIns.putIfAbsent(database, () async {
      try {
        final linked = await DatabaseLoginDialog.showLink(
          context,
          apiClient,
          database: database,
          usedBy: solutionName.isEmpty ? 'this solution' : solutionName,
        );
        if (linked) {
          _tables.clear();
          notifyListeners();
        }
        return linked;
      } finally {
        unawaited(_signIns.remove(database));
      }
    });
  }

  /// Asks once per solution, the first time a view meets it not signed in;
  /// after that the view offers to sign in.
  void askOnce(BuildContext context, ExternalUnavailable problem) {
    if (!problem.isNotSignedIn || !_asked.add(problem.database)) return;
    unawaited(signIn(context, problem));
  }
}

/// What a view shows when the other solution cannot be read: the reason and
/// the remedy, and, when it is only not signed in, a way to sign in.
class ExternalProblemNotice extends StatelessWidget {
  const ExternalProblemNotice({
    required this.problem,
    required this.access,
    this.compact = false,
    super.key,
  });

  final ExternalUnavailable problem;
  final ExternalAccess access;

  /// A field box: one line, the message in a tooltip.
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final signIn = problem.isNotSignedIn
        ? TextButton(
            key: const ValueKey('external-sign-in'),
            style: TextButton.styleFrom(
              visualDensity: VisualDensity.compact,
              padding: const EdgeInsets.symmetric(horizontal: 6),
              minimumSize: const Size(0, 24),
            ),
            onPressed: () => unawaited(access.signIn(context, problem)),
            child: Text('Sign in to ${problem.database}…', style: const TextStyle(fontSize: 11)),
          )
        : null;
    final label = problem.isNotSignedIn
        ? '<Not Signed In>'
        : problem.reason == ExternalUnavailable.noAccess
            ? '<No Access>'
            : '<File Missing>';
    if (compact) {
      return Tooltip(
        message: problem.message,
        child: Container(
          key: const ValueKey('external-problem'),
          alignment: Alignment.centerLeft,
          padding: const EdgeInsets.symmetric(horizontal: 8),
          decoration: BoxDecoration(
            color: Colors.amber.withValues(alpha: 0.10),
            border: Border.all(color: Colors.amber),
            borderRadius: BorderRadius.circular(4),
          ),
          child: Row(
            children: [
              Expanded(
                child: Text(label,
                    overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 11, color: Colors.amber)),
              ),
              ?signIn,
            ],
          ),
        ),
      );
    }
    return Container(
      key: const ValueKey('external-problem'),
      alignment: Alignment.center,
      padding: const EdgeInsets.all(8),
      color: Colors.amber.withValues(alpha: 0.10),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(problem.message, textAlign: TextAlign.center, style: const TextStyle(fontSize: 11)),
          ?signIn,
        ],
      ),
    );
  }
}
