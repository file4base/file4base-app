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
  factory ExternalAccess.of(ApiClient apiClient) {
    final token = apiClient.authToken ?? '';
    // A client with no session yet shares nothing with another one.
    if (token.isEmpty) return _unsigned[apiClient] ??= ExternalAccess._(apiClient);
    return _bySession.putIfAbsent(token, () => ExternalAccess._(apiClient));
  }
  static final Map<String, ExternalAccess> _bySession = {};
  static final Expando<ExternalAccess> _unsigned = Expando();

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

  /// Forgets the tables read so far, after a file reference was remapped or
  /// bound, so every view reads them again.
  void refresh() {
    _tables.clear();
    notifyListeners();
  }

  /// Asks for an account of the solution [problem] names and signs the
  /// session in to it. Every view that meets the same solution shares one
  /// dialog; once signed in, they read again.
  Future<bool> signIn(BuildContext context, ExternalUnavailable problem) => signInTo(context, problem.database);

  /// Asks for an account of [database] and signs the session in to it, with
  /// one dialog however many views ask at once.
  Future<bool> signInTo(BuildContext context, String database) {
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

  /// The token of the session this one opened in [database], for a script
  /// step that reaches that solution: Perform Script and Open File through a
  /// file reference, Import and Export Records through a transfer (the
  /// runner's peerTokenProvider). With none yet, it asks for an account of
  /// that solution, as a read does. Null when the user cancels: the step
  /// then stops and says so.
  ///
  /// A token kept from earlier is checked first, with one light request
  /// (`GET /api/v1/auth/session` there): one that solution no longer accepts (`401`: the
  /// account there was disabled or changed its password, or that session
  /// ended) is dropped and the user asked to sign in again, rather than
  /// handed to a step that would fail with it.
  Future<String?> peerToken(BuildContext context, String database) async {
    final known = apiClient.linkedToken(database);
    if (known != null && known.isNotEmpty) {
      if (await _accepted(known)) return known;
      peerRejected(database);
    }
    if (!context.mounted || !await signInTo(context, database)) return null;
    return apiClient.linkedToken(database);
  }

  /// Whether the solution a linked [token] was opened in still accepts it.
  Future<bool> _accepted(String token) async {
    try {
      await apiClient.forPeerSession(token).sessionCapabilities();
    } on ApiException catch (e) {
      // Anything but a refused session is the step's to meet and report.
      return e.statusCode != 401;
    } on Object {
      // Likewise: unreachable now is not signed out.
    }
    return true;
  }

  /// Forgets the session held in [database], which answered `401`: the
  /// next read or step that needs it asks for an account there again.
  void peerRejected(String database) {
    apiClient.forgetLinked(database);
    _tables.clear();
    notifyListeners();
  }

  /// Runs [call], a write into another solution, and when the session is not
  /// signed in to that solution, signs it in with the same dialog a read
  /// uses and runs [call] once more. Any other failure, and a second one, is
  /// the caller's to show.
  Future<T> retryingSignIn<T>(BuildContext context, Future<T> Function() call) async {
    try {
      return await call();
    } on ApiException catch (e) {
      final problem = ExternalUnavailable.of(e);
      if (problem == null || !problem.isNotSignedIn || !context.mounted) rethrow;
      if (!await signIn(context, problem)) rethrow;
      return call();
    }
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
