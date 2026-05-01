import 'package:dio/dio.dart';
import 'package:riverpod/riverpod.dart';
import '../../../../core/network/dio_client.dart';
import '../models/user.dart';

final userRepositoryProvider = Provider((ref) {
  final dio = ref.read(dioProvider);
  return UserRepository(dio);
});

class UserRepository {
  final Dio _dio;

  UserRepository(this._dio);

  Future<User> getProfile(String userId) async {
    final response = await _dio.get('/api/v1/user/profile/$userId');
    return User.fromJson(response.data);
  }

  Future<User> updateProfile({
    required String userId,
    String? fullName,
    String? phone,
    String? avatarUrl,
  }) async {
    final response = await _dio.put('/api/v1/user/profile/$userId', data: {
      if (fullName != null) 'full_name': fullName,
      if (phone != null) 'phone': phone,
      if (avatarUrl != null) 'avatar_url': avatarUrl,
    });
    return User.fromJson(response.data);
  }
}