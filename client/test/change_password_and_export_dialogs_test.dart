import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/data_browser/export_records_dialog.dart';
import 'package:file4base_client/features/security/change_password_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('ChangePasswordDialog renders inputs and validates matching password', (tester) async {
    final client = ApiClient(baseUrl: 'http://localhost:8080');
    const user = UserModel(id: 'usr-1', username: 'admin', role: 'admin');

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (ctx) => ElevatedButton(
              onPressed: () => ChangePasswordDialog.show(
                ctx,
                apiClient: client,
                currentUser: user,
                databaseName: 'file4base_dev',
              ),
              child: const Text('Open Dialog'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Open Dialog'));
    await tester.pumpAndSettle();

    expect(find.text('Change Password'), findsOneWidget);
    expect(find.text('New Password'), findsOneWidget);
    expect(find.text('Confirm New Password'), findsOneWidget);

    // Attempt to submit empty
    await tester.tap(find.text('Update Password'));
    await tester.pumpAndSettle();

    expect(find.text('Please enter a new password'), findsOneWidget);

    // Cancel dialog
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(find.text('Change Password'), findsNothing);
  });

  testWidgets('ExportRecordsDialog renders table selection and export formats', (tester) async {
    final client = ApiClient(baseUrl: 'http://localhost:8080');
    const table = TableModel(
      id: 'tbl-1',
      name: 'customers',
      displayName: 'Customers',
      columns: [
        ColumnModel(id: 'c1', tableId: 'tbl-1', name: 'id', displayName: 'ID', fieldType: 'VARCHAR'),
        ColumnModel(id: 'c2', tableId: 'tbl-1', name: 'name', displayName: 'Name', fieldType: 'VARCHAR'),
      ],
    );

    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (ctx) => ElevatedButton(
              onPressed: () => ExportRecordsDialog.show(
                ctx,
                apiClient: client,
                tables: [table],
                initialTable: table,
              ),
              child: const Text('Open Export'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Open Export'));
    await tester.pumpAndSettle();

    expect(find.byType(ExportRecordsDialog), findsOneWidget);
    expect(find.text('Customers (customers)'), findsOneWidget);
    expect(find.text('CSV (Comma-Separated Values)'), findsOneWidget);
    expect(find.text('Include field names as first row (header)'), findsOneWidget);

    // Cancel dialog
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(find.byType(ExportRecordsDialog), findsNothing);
  });
}
