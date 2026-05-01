import 'package:dio/dio.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:riverpod/riverpod.dart';
import '../../../../core/network/dio_client.dart';

final authRepositoryProvider = Provider((ref) {
  final dio = ref.read(dioProvider);
  return AuthRepository(dio);
});

class AuthRepository {
  final Dio _dio;
  final _storage = const FlutterSecureStorage();

  AuthRepository(this._dio);

  Future<AuthTokens> register(String email, String phone, String password, String fullName) async {
    final response = await _dio.post('/api/v1/auth/register', data: {
      'email': email,
      'phone': phone,
      'password': password,
      'full_name': fullName,
      'role': 'rider',
    });
    final tokens = AuthTokens.fromJson(response.data);
    await _storage.write(key: 'access_token', value: tokens.accessToken);
    await _storage.write(key: 'refresh_token', value: tokens.refreshToken);
    return tokens;
  }

  Future<AuthTokens> login(String email, String password) async {
    final response = await _dio.post('/api/v1/auth/login', data: {
      'email': email,
      'password': password,
    });
    final tokens = AuthTokens.fromJson(response.data);
    await _storage.write(key: 'access_token', value: tokens.accessToken);
    await _storage.write(key: 'refresh_token', value: tokens.refreshToken);
    return tokens;
  }

  Future<void> logout() async {
    await _storage.deleteAll();
  }
}

class AuthTokens {
  final String accessToken;
  final String refreshToken;
  final String userId;
  final int expiresIn;

  AuthTokens({
    required this.accessToken,
    required this.refreshToken,
    required this.userId,
    required this.expiresIn,
  });

  factory AuthTokens.fromJson(Map<String, dynamic> json) {
    return AuthTokens(
      accessToken: json['access_token'],
      refreshToken: json['refresh_token'],
      userId: json['user_id'],
      expiresIn: json['expires_in'],
    );
  }
}