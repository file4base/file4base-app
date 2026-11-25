import 'package:file4base_client/core/api/api_client.dart';
import 'package:flutter/material.dart';

/// The compose window of Send Mail with its dialog, when the mail goes
/// through the solution's SMTP profile (#133): the step's addressees,
/// subject and message, for the user to check or change before sending.
class MailComposeDialog extends StatefulWidget {
  const MailComposeDialog({required this.mail, required this.from, this.attachments = const [], super.key});

  final OutgoingMail mail;

  /// Who the message leaves as: the SMTP profile's sender.
  final String from;

  /// The names of the attached files.
  final List<String> attachments;

  /// The message as the user sends it, or null when they cancel.
  static Future<OutgoingMail?> show(
    BuildContext context, {
    required OutgoingMail mail,
    required String from,
    List<String> attachments = const [],
  }) =>
      showDialog<OutgoingMail>(
        context: context,
        barrierDismissible: false,
        builder: (_) => MailComposeDialog(mail: mail, from: from, attachments: attachments),
      );

  @override
  State<MailComposeDialog> createState() => _MailComposeDialogState();
}

class _MailComposeDialogState extends State<MailComposeDialog> {
  late final _to = TextEditingController(text: widget.mail.to);
  late final _cc = TextEditingController(text: widget.mail.cc);
  late final _bcc = TextEditingController(text: widget.mail.bcc);
  late final _subject = TextEditingController(text: widget.mail.subject);
  // FileMaker's ¶ is a carriage return: the field shows it as a new line.
  late final _message = TextEditingController(text: widget.mail.message.replaceAll('\r\n', '\n').replaceAll('\r', '\n'));
  late bool _showCopies = widget.mail.cc.isNotEmpty || widget.mail.bcc.isNotEmpty;

  @override
  void dispose() {
    for (final c in [_to, _cc, _bcc, _subject, _message]) {
      c.dispose();
    }
    super.dispose();
  }

  bool get _canSend => '${_to.text}${_cc.text}${_bcc.text}'.trim().isNotEmpty;

  Widget _field(String label, TextEditingController c, {Key? key}) => Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: TextField(
          key: key,
          controller: c,
          decoration: InputDecoration(labelText: label, isDense: true, border: const OutlineInputBorder()),
          onChanged: (_) => setState(() {}),
        ),
      );

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Send Mail'),
      content: SizedBox(
        width: 560,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Text('From: ${widget.from}', style: Theme.of(context).textTheme.bodySmall),
              ),
              Row(
                children: [
                  Expanded(child: _field('To', _to, key: const Key('mail-to'))),
                  if (!_showCopies)
                    TextButton(onPressed: () => setState(() => _showCopies = true), child: const Text('CC/BCC')),
                ],
              ),
              if (_showCopies) ...[
                _field('CC', _cc),
                _field('BCC', _bcc),
              ],
              _field('Subject', _subject, key: const Key('mail-subject')),
              TextField(
                key: const Key('mail-message'),
                controller: _message,
                minLines: 6,
                maxLines: 14,
                decoration: const InputDecoration(labelText: 'Message', border: OutlineInputBorder(), alignLabelWithHint: true),
              ),
              if (widget.attachments.isNotEmpty) ...[
                const SizedBox(height: 12),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    for (final name in widget.attachments)
                      Chip(avatar: const Icon(Icons.attach_file, size: 16), label: Text(name)),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
        FilledButton.icon(
          key: const Key('mail-send'),
          icon: const Icon(Icons.send, size: 16),
          onPressed: _canSend
              ? () => Navigator.of(context).pop(OutgoingMail(
                    to: _to.text.trim(),
                    cc: _cc.text.trim(),
                    bcc: _bcc.text.trim(),
                    subject: _subject.text,
                    message: _message.text,
                  ))
              : null,
          label: const Text('Send'),
        ),
      ],
    );
  }
}
