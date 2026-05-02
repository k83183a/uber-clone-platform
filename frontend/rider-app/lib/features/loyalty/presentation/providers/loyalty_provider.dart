import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../data/repositories/loyalty_repository.dart';
import '../../data/models/loyalty.dart';

final loyaltyProvider = StateNotifierProvider<LoyaltyNotifier, LoyaltyState>((ref) {
  final repo = ref.read(loyaltyRepositoryProvider);
  return LoyaltyNotifier(repo);
});

final rewardsProvider = FutureProvider<List<Reward>>((ref) async {
  final repo = ref.read(loyaltyRepositoryProvider);
  return await repo.getRewards();
});

class LoyaltyNotifier extends StateNotifier<LoyaltyState> {
  final LoyaltyRepository _repository;

  LoyaltyNotifier(this._repository) : super(const LoyaltyState.initial());

  Future<void> loadAccount() async {
    state = const LoyaltyState.loading();
    try {
      final account = await _repository.getAccount();
      state = LoyaltyState.loaded(account);
    } catch (e) {
      state = LoyaltyState.error(e.toString());
    }
  }

  Future<void> redeemReward(String rewardId) async {
    try {
      await _repository.redeemReward(rewardId);
      await loadAccount();
    } catch (e) {
      state = LoyaltyState.error(e.toString());
    }
  }
}

class LoyaltyState {
  final bool isLoading;
  final LoyaltyAccount? account;
  final String? error;

  const LoyaltyState._({required this.isLoading, this.account, this.error});

  const LoyaltyState.initial() : this._(isLoading: false);
  const LoyaltyState.loading() : this._(isLoading: true);
  const LoyaltyState.loaded(LoyaltyAccount account) : this._(isLoading: false, account: account);
  const LoyaltyState.error(String error) : this._(isLoading: false, error: error);
}