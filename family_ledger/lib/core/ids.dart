import 'dart:math';

final Random _rand = Random.secure();

String _hex(int value, int length) =>
    value.toRadixString(16).padLeft(length, '0');

/// 生成 UUIDv7：前 48 位是毫秒时间戳，后面是随机位。
/// 客户端离线建账时用它当主键，联网后直接 upsert，天然幂等。
String newId() {
  final ms = DateTime.now().millisecondsSinceEpoch;
  final bytes = List<int>.generate(16, (_) => _rand.nextInt(256));

  bytes[0] = (ms >> 40) & 0xff;
  bytes[1] = (ms >> 32) & 0xff;
  bytes[2] = (ms >> 24) & 0xff;
  bytes[3] = (ms >> 16) & 0xff;
  bytes[4] = (ms >> 8) & 0xff;
  bytes[5] = ms & 0xff;
  bytes[6] = 0x70 | (bytes[6] & 0x0f); // version 7
  bytes[8] = 0x80 | (bytes[8] & 0x3f); // variant 10xx

  final b = bytes.map((v) => _hex(v, 2)).join();
  return '${b.substring(0, 8)}-${b.substring(8, 12)}-${b.substring(12, 16)}-'
      '${b.substring(16, 20)}-${b.substring(20)}';
}
