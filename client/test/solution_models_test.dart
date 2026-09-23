import 'package:file4base_client/core/models/solution_models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('SolutionPackage MessagePack serialization and deserialization', () {
    final pkg = SolutionPackage(
      solutionName: 'Inventory Pro',
      databaseConnection: const DatabaseConnectionConfig(
        database: 'inventory_db',
        host: 'localhost',
        port: 5432,
        user: 'file4base',
      ),
      tables: [
        {
          'id': 't1',
          'name': 'products',
          'display_name': 'Products',
          'columns': [
            {'name': 'sku', 'field_type': 'TEXT'},
            {'name': 'price', 'field_type': 'NUMBER'},
          ],
        }
      ],
      layouts: [
        {
          'id': 'l1',
          'name': 'Product Detail',
          'definition': {'width': 800, 'theme': 'Modern'},
        }
      ],
    );

    // Pack to MessagePack
    final bytes = pkg.toMsgPack();
    expect(bytes, isNotEmpty);

    // Unpack from MessagePack
    final restored = SolutionPackage.fromMsgPack(bytes);
    expect(restored.solutionName, 'Inventory Pro');
    expect(restored.databaseConnection.database, 'inventory_db');
    expect(restored.databaseConnection.port, 5432);
    expect(restored.tables.length, 1);
    expect(restored.tables.first['name'], 'products');
    expect(restored.layouts.length, 1);
    expect(restored.layouts.first['name'], 'Product Detail');
  });

  test('DatabaseConnectionConfig encodes credentials and avoids plaintext', () {
    const rawUser = 'my_admin_user';
    const rawPass = 'super_secret_p@ssw0rd!#';

    const config = DatabaseConnectionConfig(
      database: 'secure_db',
      host: 'db.example.com',
      port: 5432,
      user: rawUser,
      password: rawPass,
    );

    // Serialization map has encoded credentials
    final map = config.toMap(encodeCredentials: true);
    expect(map['user'], isNot(rawUser));
    expect(map['password'], isNot(rawPass));
    expect(map['user'].toString().startsWith('enc:'), isTrue);
    expect(map['password'].toString().startsWith('enc:'), isTrue);

    // Deserialization restores plain text
    final decoded = DatabaseConnectionConfig.fromMap(map);
    expect(decoded.user, rawUser);
    expect(decoded.password, rawPass);
    expect(decoded.database, 'secure_db');
  });
}
