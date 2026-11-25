import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

/// The message of a validation error the person may override (FileMaker's
/// "Allow user to override during data entry"): every rule the values break
/// lets them keep the values. Null for any other error.
String? overridableValidationMessage(Object error) {
  if (error is! ApiException || error.statusCode != 422) return null;
  final params = error.raw['invalid_params'];
  if (params is! List || params.isEmpty) return null;
  final all = params.every((p) => p is Map && p['overridable'] == true);
  if (!all) return null;
  return error.detail.isNotEmpty ? error.detail : 'This value does not meet the validation requirements.';
}

/// Asks, as FileMaker does, whether to keep a value that breaks a validation
/// rule allowing it: Yes keeps it, No goes back to the field.
Future<bool> confirmValidationOverride(BuildContext context, String message) async {
  final keep = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('Validation'),
      content: Text('$message\n\nAllow this value?'),
      actions: [
        TextButton(
          key: const Key('validationOverrideNo'),
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('No'),
        ),
        FilledButton(
          key: const Key('validationOverrideYes'),
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('Yes'),
        ),
      ],
    ),
  );
  return keep ?? false;
}

/// Runs a data-entry write; when the server refuses it for rules the person
/// may override, asks them, and sends it again with the override if they
/// keep the values. Any other error, or a No, is thrown as it was.
Future<T> writeAllowingOverride<T>(
  BuildContext context,
  Future<T> Function({required bool overrideValidation}) write,
) async {
  try {
    return await write(overrideValidation: false);
  } on ApiException catch (e) {
    final message = overridableValidationMessage(e);
    if (message == null || !context.mounted) rethrow;
    if (!await confirmValidationOverride(context, message)) rethrow;
    return write(overrideValidation: true);
  }
}
