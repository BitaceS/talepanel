import 'package:dio/dio.dart';
import 'package:dio_cookie_manager/dio_cookie_manager.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

// ─── Models ───────────────────────────────────────────────────────────────────

class User {
  final String id;
  final String email;
  final String username;
  final String role;
  final bool totpEnabled;

  const User({
    required this.id,
    required this.email,
    required this.username,
    required this.role,
    required this.totpEnabled,
  });

  factory User.fromJson(Map<String, dynamic> json) => User(
        id: json['id'] as String,
        email: json['email'] as String,
        username: json['username'] as String,
        role: json['role'] as String,
        totpEnabled: json['totp_enabled'] as bool? ?? false,
      );
}

class Server {
  final String id;
  final String name;
  final String status;
  final String hytaleVersion;
  final int port;
  final bool autoRestart;
  final int? ramLimitMb;
  final String? activeWorld;

  const Server({
    required this.id,
    required this.name,
    required this.status,
    required this.hytaleVersion,
    required this.port,
    required this.autoRestart,
    this.ramLimitMb,
    this.activeWorld,
  });

  factory Server.fromJson(Map<String, dynamic> json) => Server(
        id: json['id'] as String,
        name: json['name'] as String,
        status: json['status'] as String,
        hytaleVersion: json['hytale_version'] as String,
        port: json['port'] as int,
        autoRestart: json['auto_restart'] as bool? ?? true,
        ramLimitMb: json['ram_limit_mb'] as int?,
        activeWorld: json['active_world'] as String?,
      );

  bool get isRunning => status == 'running';
  bool get isStopped => status == 'stopped';
  bool get isCrashed => status == 'crashed';
}

// ─── API Service ──────────────────────────────────────────────────────────────

class ApiService {
  final Dio _dio;
  final FlutterSecureStorage _storage;
  final CookieJar _cookieJar;

  String? _accessToken;
  String _baseUrl;

  ApiService({String baseUrl = 'http://localhost:8080'})
      : _baseUrl = baseUrl,
        _storage = const FlutterSecureStorage(),
        _cookieJar = CookieJar(),
        _dio = Dio(BaseOptions(
          connectTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 30),
          headers: {'User-Agent': 'TalePanelMobile/0.1.0'},
        )) {
    _dio.interceptors.add(CookieManager(_cookieJar));
    _dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          if (_accessToken != null) {
            options.headers['Authorization'] = 'Bearer $_accessToken';
          }
          handler.next(options);
        },
        onError: (error, handler) async {
          if (error.response?.statusCode == 401 && _accessToken != null) {
            // Try to refresh
            try {
              await _refresh();
              // Retry original request
              final opts = error.requestOptions;
              opts.headers['Authorization'] = 'Bearer $_accessToken';
              final response = await _dio.fetch(opts);
              handler.resolve(response);
              return;
            } catch (_) {
              await logout();
            }
          }
          handler.next(error);
        },
      ),
    );
  }

  String get baseUrl => _baseUrl;
  bool get isAuthenticated => _accessToken != null;

  Future<void> initialize() async {
    _baseUrl = await _storage.read(key: 'api_url') ?? _baseUrl;
    _accessToken = await _storage.read(key: 'access_token');
  }

  Future<User> login(String email, String password) async {
    final resp = await _dio.post(
      '$_baseUrl/api/v1/auth/login',
      data: {'email': email, 'password': password},
    );

    final token = resp.data['access_token'] as String?;
    if (token == null) throw Exception('Login failed: no token returned');

    _accessToken = token;
    await _storage.write(key: 'access_token', value: token);
    await _storage.write(key: 'api_url', value: _baseUrl);

    return User.fromJson(resp.data['user'] as Map<String, dynamic>);
  }

  Future<void> _refresh() async {
    final resp = await _dio.post('$_baseUrl/api/v1/auth/refresh');
    final token = resp.data['access_token'] as String?;
    if (token == null) throw Exception('Refresh failed');
    _accessToken = token;
    await _storage.write(key: 'access_token', value: token);
  }

  Future<void> logout() async {
    try {
      await _dio.post('$_baseUrl/api/v1/auth/logout');
    } catch (_) {}
    _accessToken = null;
    await _storage.deleteAll();
    _cookieJar.deleteAll();
  }

  Future<User> getMe() async {
    final resp = await _dio.get('$_baseUrl/api/v1/auth/me');
    return User.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<List<Server>> getServers() async {
    final resp = await _dio.get('$_baseUrl/api/v1/servers');
    final list = resp.data as List<dynamic>;
    return list.map((s) => Server.fromJson(s as Map<String, dynamic>)).toList();
  }

  Future<Server> getServer(String id) async {
    final resp = await _dio.get('$_baseUrl/api/v1/servers/$id');
    return Server.fromJson(resp.data as Map<String, dynamic>);
  }

  Future<void> startServer(String id) async {
    await _dio.post('$_baseUrl/api/v1/servers/$id/start');
  }

  Future<void> stopServer(String id) async {
    await _dio.post('$_baseUrl/api/v1/servers/$id/stop');
  }

  Future<void> restartServer(String id) async {
    await _dio.post('$_baseUrl/api/v1/servers/$id/restart');
  }

  Future<void> killServer(String id) async {
    await _dio.post('$_baseUrl/api/v1/servers/$id/kill');
  }
}

// ─── Providers ────────────────────────────────────────────────────────────────

final apiServiceProvider = Provider<ApiService>((ref) {
  return ApiService();
});
