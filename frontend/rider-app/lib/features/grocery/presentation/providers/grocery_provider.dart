import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../data/repositories/grocery_repository.dart';
import '../../data/models/store.dart';

final groceryProvider = StateNotifierProvider<GroceryNotifier, GroceryState>((ref) {
  final repo = ref.read(groceryRepositoryProvider);
  return GroceryNotifier(repo);
});

class GroceryNotifier extends StateNotifier<GroceryState> {
  final GroceryRepository _repository;

  GroceryNotifier(this._repository) : super(const GroceryState.initial());

  Future<void> loadStores() async {
    state = const GroceryState.loading();
    try {
      final stores = await _repository.getStores();
      state = GroceryState.loaded(stores);
    } catch (e) {
      state = GroceryState.error(e.toString());
    }
  }

  Future<void> searchStores(String query) async {
    if (query.isEmpty) {
      await loadStores();
      return;
    }
    state = const GroceryState.loading();
    try {
      final stores = await _repository.searchStores(query);
      state = GroceryState.loaded(stores);
    } catch (e) {
      state = GroceryState.error(e.toString());
    }
  }

  Future<void> filterByCategory(String category) async {
    state = const GroceryState.loading();
    try {
      final stores = await _repository.filterByCategory(category);
      state = GroceryState.loaded(stores);
    } catch (e) {
      state = GroceryState.error(e.toString());
    }
  }
}

class GroceryState {
  final bool isLoading;
  final List<Store>? stores;
  final String? error;

  const GroceryState._({required this.isLoading, this.stores, this.error});

  const GroceryState.initial() : this._(isLoading: false);
  const GroceryState.loading() : this._(isLoading: true);
  const GroceryState.loaded(List<Store> stores) : this._(isLoading: false, stores: stores);
  const GroceryState.error(String error) : this._(isLoading: false, error: error);
}