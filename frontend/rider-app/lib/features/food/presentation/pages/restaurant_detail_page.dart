import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:cached_network_image/cached_network_image.dart';
import '../providers/food_provider.dart';
import '../providers/cart_provider.dart';
import '../models/menu_item.dart';

class RestaurantDetailPage extends ConsumerStatefulWidget {
  final String restaurantId;
  const RestaurantDetailPage({super.key, required this.restaurantId});

  @override
  ConsumerState<RestaurantDetailPage> createState() => _RestaurantDetailPageState();
}

class _RestaurantDetailPageState extends ConsumerState<RestaurantDetailPage> {
  String _selectedCategory = 'All';
  List<String> _categories = ['All'];

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(restaurantDetailProvider(widget.restaurantId).notifier).loadRestaurant();
      ref.read(menuItemsProvider(widget.restaurantId).notifier).loadMenu();
    });
  }

  @override
  Widget build(BuildContext context) {
    final restaurantState = ref.watch(restaurantDetailProvider(widget.restaurantId));
    final menuState = ref.watch(menuItemsProvider(widget.restaurantId));
    final cartItems = ref.watch(cartProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Restaurant Details')),
      body: Column(
        children: [
          restaurantState.when(
            loading: () => const Center(child: CircularProgressIndicator()),
            loaded: (restaurant) => _RestaurantHeader(restaurant: restaurant),
            error: (error) => Center(child: Text('Error: $error')),
          ),
          // Categories
          menuState.when(
            loading: () => const SizedBox(),
            loaded: (items) {
              // Extract unique categories
              final allCategories = ['All', ...items.map((i) => i.category).toSet().toList()];
              if (_categories.length != allCategories.length) {
                _categories = allCategories;
              }
              return SizedBox(
                height: 48,
                child: ListView.builder(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 12),
                  itemCount: _categories.length,
                  itemBuilder: (context, index) {
                    final category = _categories[index];
                    final isSelected = _selectedCategory == category;
                    return Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 4),
                      child: FilterChip(
                        label: Text(category),
                        selected: isSelected,
                        onSelected: (selected) {
                          setState(() => _selectedCategory = category);
                          ref.read(menuItemsProvider(widget.restaurantId).notifier).filterByCategory(
                            category == 'All' ? '' : category,
                          );
                        },
                        backgroundColor: Colors.grey[200],
                        selectedColor: Colors.green[100],
                      ),
                    );
                  },
                ),
              );
            },
            error: (error) => const SizedBox(),
          ),
          Expanded(
            child: menuState.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              loaded: (items) => ListView.builder(
                padding: const EdgeInsets.all(12),
                itemCount: items.length,
                itemBuilder: (context, index) {
                  final item = items[index];
                  return _MenuItemCard(
                    item: item,
                    quantity: cartItems.firstWhere(
                      (i) => i.id == item.id,
                      orElse: () => CartItem(id: item.id, name: item.name, price: item.price, quantity: 0),
                    ).quantity,
                    onAdd: () => ref.read(cartProvider.notifier).addItem(item, 1),
                    onRemove: () => ref.read(cartProvider.notifier).removeItem(item.id),
                  );
                },
              ),
              error: (error) => Center(child: Text('Error: $error')),
            ),
          ),
          if (cartItems.isNotEmpty)
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: Colors.white,
                boxShadow: [BoxShadow(color: Colors.grey.withOpacity(0.2), blurRadius: 8)],
              ),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${cartItems.length} items • £${cartItems.fold(0.0, (sum, i) => sum + (i.price * i.quantity)).toStringAsFixed(2)}',
                          style: const TextStyle(fontWeight: FontWeight.bold),
                        ),
                        const Text('Includes delivery fee and tax', style: TextStyle(fontSize: 12, color: Colors.grey)),
                      ],
                    ),
                  ),
                  ElevatedButton(
                    onPressed: () => Navigator.push(
                      context,
                      MaterialPageRoute(
                        builder: (_) => CartPage(restaurantId: widget.restaurantId),
                      ),
                    ),
                    style: ElevatedButton.styleFrom(backgroundColor: Colors.green),
                    child: const Text('View Cart'),
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _RestaurantHeader extends StatelessWidget {
  final Restaurant restaurant;
  const _RestaurantHeader({required this.restaurant});

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Stack(
          children: [
            CachedNetworkImage(
              imageUrl: restaurant.imageUrl,
              height: 180,
              width: double.infinity,
              fit: BoxFit.cover,
              placeholder: (context, url) => Container(
                height: 180,
                color: Colors.grey[200],
                child: const Center(child: CircularProgressIndicator()),
              ),
              errorWidget: (context, url, error) => Container(
                height: 180,
                color: Colors.grey[200],
                child: const Icon(Icons.restaurant, size: 50, color: Colors.grey),
              ),
            ),
            Container(
              height: 180,
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: [Colors.transparent, Colors.black.withOpacity(0.5)],
                ),
              ),
            ),
            Positioned(
              bottom: 16,
              left: 16,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    restaurant.name,
                    style: const TextStyle(color: Colors.white, fontSize: 24, fontWeight: FontWeight.bold),
                  ),
                  Row(
                    children: [
                      const Icon(Icons.star, size: 16, color: Colors.amber),
                      const SizedBox(width: 4),
                      Text(
                        restaurant.rating.toStringAsFixed(1),
                        style: const TextStyle(color: Colors.white),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        '${restaurant.deliveryTime} min',
                        style: const TextStyle(color: Colors.white),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        '£${restaurant.deliveryFee.toStringAsFixed(2)} delivery',
                        style: const TextStyle(color: Colors.white),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
        Padding(
          padding: const EdgeInsets.all(12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(restaurant.description, style: const TextStyle(color: Colors.grey)),
              const SizedBox(height: 8),
              Row(
                children: [
                  const Icon(Icons.location_on, size: 16, color: Colors.grey),
                  const SizedBox(width: 4),
                  Expanded(child: Text(restaurant.address, style: const TextStyle(color: Colors.grey, fontSize: 12))),
                ],
              ),
            ],
          ),
        ),
        const Divider(),
      ],
    );
  }
}

class _MenuItemCard extends StatelessWidget {
  final MenuItem item;
  final int quantity;
  final VoidCallback onAdd;
  final VoidCallback onRemove;

  const _MenuItemCard({
    required this.item,
    required this.quantity,
    required this.onAdd,
    required this.onRemove,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      if (item.isVegetarian)
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                          decoration: BoxDecoration(
                            color: Colors.green[100],
                            borderRadius: BorderRadius.circular(4),
                          ),
                          child: const Text('Veg', style: TextStyle(fontSize: 10, color: Colors.green)),
                        ),
                      if (item.isSpicy)
                        const SizedBox(width: 4),
                      if (item.isSpicy)
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                          decoration: BoxDecoration(
                            color: Colors.red[100],
                            borderRadius: BorderRadius.circular(4),
                          ),
                          child: const Text('Spicy', style: TextStyle(fontSize: 10, color: Colors.red)),
                        ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(item.name, style: const TextStyle(fontWeight: FontWeight.bold)),
                  const SizedBox(height: 4),
                  Text(item.description, style: const TextStyle(fontSize: 12, color: Colors.grey)),
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      Text(
                        '£${item.price.toStringAsFixed(2)}',
                        style: const TextStyle(fontWeight: FontWeight.bold, color: Colors.green),
                      ),
                      if (item.discountPrice > 0) ...[
                        const SizedBox(width: 8),
                        Text(
                          '£${item.discountPrice.toStringAsFixed(2)}',
                          style: TextStyle(decoration: TextDecoration.lineThrough, color: Colors.grey),
                        ),
                      ],
                    ],
                  ),
                ],
              ),
            ),
            if (quantity > 0)
              Row(
                children: [
                  IconButton(onPressed: onRemove, icon: const Icon(Icons.remove, color: Colors.green)),
                  Text(quantity.toString(), style: const TextStyle(fontWeight: FontWeight.bold)),
                  IconButton(onPressed: onAdd, icon: const Icon(Icons.add, color: Colors.green)),
                ],
              )
            else
              ElevatedButton(
                onPressed: onAdd,
                style: ElevatedButton.styleFrom(backgroundColor: Colors.green),
                child: const Text('Add'),
              ),
          ],
        ),
      ),
    );
  }
}