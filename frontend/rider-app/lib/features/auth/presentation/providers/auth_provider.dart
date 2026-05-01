import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../user/data/repositories/user_repository.dart';
import '../../data/repositories/auth_repository.dart';

final authProvider = StateNotifierProvider<AuthNotifier, AuthState>((ref) {
  final authRepo = ref.read(authRepositoryProvider);
  final userRepo = ref.read(userRepositoryProvider);
  return AuthNotifier(authRepo, userRepo);
});

class AuthNotifier extends StateNotifier<AuthState> {
  final AuthRepository _authRepo;
  final UserRepository _userRepo;

  AuthNotifier(this._authRepo, this._userRepo) : super(const AuthState.unauthenticated());

  Future<void> login(String email, String password) async {
    state = const AuthState.loading();
    try {
      final tokens = await _authRepo.login(email, password);
      final user = await _userRepo.getProfile(tokens.userId);
      state = AuthState.authenticated(user);
    } catch (e) {
      state = AuthState.error(e.toString());
    }
  }

  Future<void> register(String email, String phone, String password, String fullName) async {
    state = const AuthState.loading();
    try {
      final tokens = await _authRepo.register(email, phone, password, fullName);
      final user = await _userRepo.getProfile(tokens.userId);
      state = AuthState.authenticated(user);
    } catch (e) {
      state = AuthState.error(e.toString());
    }
  }

  Future<void> logout() async {
    await _authRepo.logout();
    state = const AuthState.unauthenticated();
  }
}

class AuthState {
  final bool isLoading;
  final bool isAuthenticated;
  final User? user;
  final String? error;

  const AuthState._({
    required this.isLoading,
    required this.isAuthenticated,
    this.user,
    this.error,
  });

  const AuthState.loading() : this._(isLoading: true, isAuthenticated: false);
  const AuthState.authenticated(User user) : this._(isLoading: false, isAuthenticated: true, user: user);
  const AuthState.unauthenticated() : this._(isLoading: false, isAuthenticated: false);
  const AuthState.error(String error) : this._(isLoading: false, isAuthenticated: false, error: error);
}