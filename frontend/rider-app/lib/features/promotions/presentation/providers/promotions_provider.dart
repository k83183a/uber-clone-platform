import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../data/repositories/promotions_repository.dart';
import '../../data/models/promotion.dart';

final promotionsProvider = StateNotifierProvider<PromotionsNotifier, PromotionsState>((ref) {
  final repo = ref.read(promotionsRepositoryProvider);
  return PromotionsNotifier(repo);
});

final aiPromotionsProvider = StateNotifierProvider<AIPromotionsNotifier, PromotionsState>((ref) {
  final repo = ref.read(promotionsRepositoryProvider);
  return AIPromotionsNotifier(repo);
});

class PromotionsNotifier extends StateNotifier<PromotionsState> {
  final PromotionsRepository _repository;

  PromotionsNotifier(this._repository) : super(const PromotionsState.initial());

  Future<void> loadPromotions() async {
    state = const PromotionsState.loading();
    try {
      final promotions = await _repository.getPromotions();
      state = PromotionsState.loaded(promotions);
    } catch (e) {
      state = PromotionsState.error(e.toString());
    }
  }

  void applyPromoCode(String code) {
    // Store applied code in shared preferences or provider
  }

  void removePromoCode() {
    // Clear applied code
  }
}

class AIPromotionsNotifier extends StateNotifier<PromotionsState> {
  final PromotionsRepository _repository;

  AIPromotionsNotifier(this._repository) : super(const PromotionsState.initial());

  Future<void> loadAIPromotions() async {
    state = const PromotionsState.loading();
    try {
      final promotions = await _repository.getAIPromotions();
      state = PromotionsState.loaded(promotions);
    } catch (e) {
      state = PromotionsState.error(e.toString());
    }
  }
}

class PromotionsState {
  final bool isLoading;
  final List<Promotion>? promotions;
  final String? error;

  const PromotionsState._({required this.isLoading, this.promotions, this.error});

  const PromotionsState.initial() : this._(isLoading: false);
  const PromotionsState.loading() : this._(isLoading: true);
  const PromotionsState.loaded(List<Promotion> promotions) : this._(isLoading: false, promotions: promotions);
  const PromotionsState.error(String error) : this._(isLoading: false, error: error);
}