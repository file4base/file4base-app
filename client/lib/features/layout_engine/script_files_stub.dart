import 'package:file4base_client/features/layout_engine/script_files.dart';

/// The browser has no disk a script may write: its folders are in memory.
ScriptFileStore createPlatformScriptFileStore() => MemoryScriptFileStore();
