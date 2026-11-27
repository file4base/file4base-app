import 'package:file4base_client/core/api/api_client.dart';
import 'package:file4base_client/features/server_tools/server_schedules_panel.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// A server administrator's API with one nightly backup schedule.
class _SchedulesApi extends ApiClient {
  _SchedulesApi() : super(baseUrl: 'http://localhost:1');

  final List<Map<String, dynamic>> schedules = [
    {
      'id': 'sch_1',
      'name': 'Nightly backup',
      'type': 'backup',
      'databases': <dynamic>[],
      'cron': '0 2 * * *',
      'keep': 7,
      'enabled': true,
      'next_run_at': '2026-11-28T01:00:00Z',
      'running': false,
      'queued': false,
      'last_run': {
        'id': 3,
        'schedule_id': 'sch_1',
        'trigger': 'schedule',
        'status': 'success',
        'started_at': '2026-11-27T01:00:00Z',
        'duration_ms': 4200,
        'size_bytes': 3 * 1024 * 1024,
      },
    },
    {
      'id': 'sch_2',
      'name': 'Weekly statistics',
      'type': 'maintenance',
      'databases': <dynamic>['crm'],
      'cron': '30 4 * * 0',
      'operation': 'analyze',
      'enabled': false,
      'next_run_at': null,
      'running': false,
      'queued': false,
      'last_run': {
        'id': 2,
        'schedule_id': 'sch_2',
        'trigger': 'schedule',
        'status': 'failed',
        'started_at': '2026-11-22T03:30:00Z',
        'duration_ms': 10,
        'size_bytes': 0,
        'error': 'crm: the solution does not exist',
      },
    },
  ];
  final List<Map<String, dynamic>> created = [];
  final List<(String, Map<String, dynamic>)> updates = [];
  final List<String> runs = [];

  @override
  Future<Map<String, dynamic>> listServerSchedules() async => {
        'schedules': schedules,
        'time_zone': 'Europe/Paris',
        'scheduler_running': true,
      };

  @override
  Future<Map<String, dynamic>> listDatabases() async => {
        'databases': <dynamic>['crm', 'stock'],
      };

  @override
  Future<Map<String, dynamic>> createServerSchedule(Map<String, dynamic> schedule) async {
    created.add(schedule);
    return {...schedule, 'id': 'sch_new'};
  }

  @override
  Future<Map<String, dynamic>> updateServerSchedule(String id, Map<String, dynamic> patch) async {
    updates.add((id, patch));
    return {'id': id, ...patch};
  }

  @override
  Future<Map<String, dynamic>> runServerScheduleNow(String id) async {
    runs.add(id);
    schedules.firstWhere((s) => s['id'] == id)['queued'] = true;
    return {'status': 'queued'};
  }
}

Future<void> _pump(WidgetTester tester, ApiClient api) async {
  tester.view.physicalSize = const Size(1400, 1000);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.resetPhysicalSize);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: ServerSchedulesPanel(apiClient: api))));
  await tester.pump();
  await tester.pump();
}

void main() {
  testWidgets('lists each schedule with its frequency, next run and last outcome', (tester) async {
    final api = _SchedulesApi();
    await _pump(tester, api);

    expect(find.text('Nightly backup'), findsOneWidget);
    expect(find.text('Daily at 02:00'), findsOneWidget);
    expect(find.text('Completed'), findsOneWidget);
    expect(find.textContaining('4 s · 3.0 MB'), findsOneWidget);
    expect(find.textContaining('Backup, keeps 7 · all solutions'), findsOneWidget);

    expect(find.text('Weekly statistics'), findsOneWidget);
    expect(find.text('Sun at 04:30'), findsOneWidget);
    expect(find.text('Disabled'), findsOneWidget);
    expect(find.text('Failed'), findsOneWidget);
    expect(find.text('crm: the solution does not exist'), findsOneWidget);
    expect(find.textContaining('Europe/Paris'), findsOneWidget);
  });

  testWidgets('enables, disables and runs a schedule now', (tester) async {
    final api = _SchedulesApi();
    await _pump(tester, api);

    await tester.tap(find.byType(Switch).first);
    await tester.pump();
    await tester.pump();
    expect(api.updates.single.$1, 'sch_1');
    expect(api.updates.single.$2, {'enabled': false});

    await tester.tap(find.byTooltip('Run now').first);
    await tester.pump();
    await tester.pump();
    expect(api.runs, ['sch_1']);
    expect(find.text('Waiting to run'), findsOneWidget);
    // A queued schedule cannot be queued twice.
    final run = tester.widget<IconButton>(
      find.ancestor(of: find.byTooltip('Run now').first, matching: find.byType(IconButton)).first,
    );
    expect(run.onPressed, isNull);
    // Its outcome is polled for while it waits.
    await tester.pump(const Duration(seconds: 4));
  });

  testWidgets('a new schedule is a backup or maintenance job, never a script', (tester) async {
    final api = _SchedulesApi();
    await _pump(tester, api);

    await tester.tap(find.text('New Schedule'));
    await tester.pumpAndSettle();
    expect(find.text('Backup'), findsOneWidget);
    expect(find.text('Maintenance'), findsOneWidget);
    expect(find.textContaining('Script'), findsNothing);

    await tester.enterText(find.byKey(const ValueKey('schedule-name')), 'Night copy');
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(api.created.single, {
      'name': 'Night copy',
      'type': 'backup',
      'databases': <String>[],
      'cron': '0 2 * * *',
      'enabled': true,
      'keep': 7,
    });
  });

  testWidgets('a backup schedule is never saved without a count of copies to keep', (tester) async {
    final api = _SchedulesApi();
    await _pump(tester, api);

    await tester.tap(find.text('New Schedule'));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextField, 'Backups to keep of each solution'), '');
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(api.created, isEmpty, reason: 'an empty count is not sent as 0, nor replaced by a default');
    expect(find.textContaining('Keep from 1 to 1000'), findsOneWidget);
  });

  test('reads the frequencies back from their cron expressions', () {
    expect(describeCron('15 * * * *'), 'Every hour at :15');
    expect(describeCron('0 2 * * *'), 'Daily at 02:00');
    expect(describeCron('0 23 * * 1,2,3,4,5'), 'Mon, Tue, Wed, Thu, Fri at 23:00');
    expect(describeCron('0 3 * * 7'), 'Sun at 03:00');
    expect(describeCron('*/10 8-18 * * 1-5'), 'Cron: */10 8-18 * * 1-5');
    expect(ScheduleTiming.parse('0 23 * * 5,1').toCron(), '0 23 * * 1,5');
    expect(ScheduleTiming.parse('0 2 1 * *').frequency, ScheduleFrequency.custom);
  });
}
