import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../data/models/menu_item.dart';

final cartProvider = StateNotifierProvider<CartNotifier, List<CartItem>>((ref) {
  return CartNotifier();
});

class CartNotifier extends StateNotifier<List<CartItem>> {
  CartNotifier() : super([]);

  void addItem(MenuItem item, int quantity) {
    final existingIndex = state.indexWhere((i) => i.id == item.id);
    if (existingIndex != -1) {
      final updated = [...state];
      updated[existingIndex] = updated[existingIndex].copyWith(
        quantity: updated[existingIndex].quantity + quantity,
      );
      state = updated;
    } else {
      state = [...state, CartItem(
        id: item.id,
        name: item.name,
        price: item.discountPrice > 0 ? item.discountPrice : item.price,
        quantity: quantity,
      )];
    }
  }

  void removeItem(String itemId) {
    final existingIndex = state.indexWhere((i) => i.id == itemId);
    if (existingIndex != -1) {
      if (state[existingIndex].quantity > 1) {
        final updated = [...state];
        updated[existingIndex] = updated[existingIndex].copyWith(
          quantity: updated[existingIndex].quantity - 1,
        );
        state = updated;
      } else {
        state = state.where((i) => i.id != itemId).toList();
      }
    }
  }

  void clearCart() {
    state = [];
  }
}

class CartItem {
  final String id;
  final String name;
  final double price;
  final int quantity;

  CartItem({
    required this.id,
    required this.name,
    required this.price,
    required this.quantity,
  });

  CartItem copyWith({String? id, String? name, double? price, int? quantity}) {
    return CartItem(
      id: id ?? this.id,
      name: name ?? this.name,
      price: price ?? this.price,
      quantity: quantity ?? this.quantity,
    );
  }
}