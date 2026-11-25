// #107 — after a sign-in, the solutions this one reads that the same account
// could not be signed in to: one unobtrusive notice listing them, each with a
// way to sign in to it with another account. Signing in to this solution
// never fails because of them.

import 'dart:async';

import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/external_access.dart';
import 'package:flutter/material.dart';

/// The solutions of [linked] a sign-in by hand could still reach.
List<LinkedSolutionModel> linkedNeedingSignIn(List<LinkedSolutionModel> linked) =>
    [for (final l in linked) if (l.canSignIn) l];

/// Shows the notice for the last sign-in of [apiClient], if any solution it
/// reaches needs signing in to. Nothing is shown otherwise.
void showLinkedSignInNotice(BuildContext context, ApiClient apiClient, {String solutionName = ''}) {
  final pending = linkedNeedingSignIn(apiClient.signInLinks);
  if (pending.isEmpty) return;
  final messenger = ScaffoldMessenger.of(context);
  messenger.hideCurrentMaterialBanner();
  messenger.showMaterialBanner(MaterialBanner(
    key: const ValueKey('linked-sign-in-notice'),
    leading: const Icon(Icons.link_off),
    content: LinkedSignInNotice(
      apiClient: apiClient,
      pending: pending,
      solutionName: solutionName,
      onDone: messenger.hideCurrentMaterialBanner,
    ),
    actions: [
      TextButton(onPressed: messenger.hideCurrentMaterialBanner, child: const Text('Dismiss')),
    ],
  ));
}

class LinkedSignInNotice extends StatefulWidget {
  const LinkedSignInNotice({
    required this.apiClient,
    required this.pending,
    this.solutionName = '',
    this.onDone,
    super.key,
  });

  final ApiClient apiClient;
  final List<LinkedSolutionModel> pending;
  final String solutionName;

  /// Called once every solution listed is signed in to.
  final VoidCallback? onDone;

  @override
  State<LinkedSignInNotice> createState() => _LinkedSignInNoticeState();
}

class _LinkedSignInNoticeState extends State<LinkedSignInNotice> {
  late final List<LinkedSolutionModel> _pending = List.of(widget.pending);

  Future<void> _signIn(LinkedSolutionModel linked) async {
    final access = ExternalAccess.of(widget.apiClient);
    if (widget.solutionName.isNotEmpty) access.solutionName = widget.solutionName;
    if (!await access.signInTo(context, linked.database) || !mounted) return;
    setState(() => _pending.remove(linked));
    if (_pending.isEmpty) widget.onDone?.call();
  }

  @override
  Widget build(BuildContext context) {
    final solution = widget.solutionName.isEmpty ? 'This solution' : '"${widget.solutionName}"';
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '$solution reads ${_pending.length == 1 ? 'a solution' : 'solutions'} your account could not be '
          'signed in to. Their tables and scripts stay unavailable until you sign in.',
          style: const TextStyle(fontSize: 12.5),
        ),
        for (final l in _pending)
          Row(
            key: ValueKey('linked-sign-in-${l.database}'),
            children: [
              Expanded(
                child: Text(
                  '${l.database}'
                  '${l.references.isEmpty ? '' : ' (${l.references.map((r) => '"$r"').join(', ')})'}: ${l.reason}',
                  style: const TextStyle(fontSize: 12),
                ),
              ),
              TextButton(
                onPressed: () => unawaited(_signIn(l)),
                child: const Text('Sign in…'),
              ),
            ],
          ),
      ],
    );
  }
}
