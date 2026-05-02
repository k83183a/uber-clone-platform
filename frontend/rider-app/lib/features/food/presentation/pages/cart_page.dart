import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/cart_provider.dart';
import '../providers/food_provider.dart';

class CartPage extends ConsumerStatefulWidget {
  final String restaurantId;
  const CartPage({super.key, required this.restaurantId});

  @override
  ConsumerState<CartPage> createState() => _CartPageState();
}

class _CartPageState extends ConsumerState<CartPage> {
  String _paymentMethod = 'card';
  String _deliveryAddress = '';
  String _specialInstructions = '';
  bool _isLoading = false;

  @override
  Widget build(BuildContext context) {
    final cartItems = ref.watch(cartProvider);
    final restaurantState = ref.watch(restaurantDetailProvider(widget.restaurantId));
    final subtotal = cartItems.fold(0.0, (sum, i) => sum + (i.price * i.quantity));
    final deliveryFee = 1.99;
    final serviceFee = subtotal * 0.05;
    final tax = subtotal * 0.20;
    final total = subtotal + deliveryFee + serviceFee + tax;

    return Scaffold(
      appBar: AppBar(title: const Text('Your Cart')),
      body: Column(
        children: [
          Expanded(
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                restaurantState.when(
                  loading: () => const SizedBox(),
                  loaded: (restaurant) => Card(
                    child: ListTile(
                      leading: ClipRRect(
                        borderRadius: BorderRadius.circular(8),
                        child: CachedNetworkImage(
                          imageUrl: restaurant.imageUrl,
                          width: 50,
                          height: 50,
                          fit: BoxFit.cover,
                        ),
                      ),
                      title: Text(restaurant.name),
                      subtitle: Text(restaurant.address),
                    ),
                  ),
                  error: (error) => const SizedBox(),
                ),
                const SizedBox(height: 16),
                const Text('Order Items', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                const SizedBox(height: 8),
                ...cartItems.map((item) => _CartItemRow(item: item)),
                const Divider(),
                _OrderSummaryRow(label: 'Subtotal', amount: subtotal),
                _OrderSummaryRow(label: 'Delivery Fee', amount: deliveryFee),
                _OrderSummaryRow(label: 'Service Fee', amount: serviceFee),
                _OrderSummaryRow(label: 'Tax (20%)', amount: tax),
                const Divider(),
                _OrderSummaryRow(label: 'Total', amount: total, isBold: true),
                const SizedBox(height: 16),
                const Text('Delivery Address', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                const SizedBox(height: 8),
                TextFormField(
                  decoration: const InputDecoration(
                    hintText: 'Enter delivery address',
                    border: OutlineInputBorder(),
                  ),
                  onChanged: (value) => _deliveryAddress = value,
                ),
                const SizedBox(height: 16),
                const Text('Special Instructions', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                const SizedBox(height: 8),
                TextFormField(
                  decoration: const InputDecoration(
                    hintText: 'Any special requests?',
                    border: OutlineInputBorder(),
                  ),
                  maxLines: 2,
                  onChanged: (value) => _specialInstructions = value,
                ),
                const SizedBox(height: 16),
                const Text('Payment Method', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                const SizedBox(height: 8),
                Row(
                  children: [
                    Expanded(
                      child: _PaymentMethodCard(
                        title: 'Card',
                        icon: Icons.credit_card,
                        isSelected: _paymentMethod == 'card',
                        onTap: () => setState(() => _paymentMethod = 'card'),
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: _PaymentMethodCard(
                        title: 'Apple Pay',
                        icon: Icons.apple,
                        isSelected: _paymentMethod == 'apple_pay',
                        onTap: () => setState(() => _paymentMethod = 'apple_pay'),
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: _PaymentMethodCard(
                        title: 'Google Pay',
                        icon: Icons.android,
                        isSelected: _paymentMethod == 'google_pay',
                        onTap: () => setState(() => _paymentMethod = 'google_pay'),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
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
                        'Total: £${total.toStringAsFixed(2)}',
                        style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                      ),
                      Text(
                        '${cartItems.length} items',
                        style: const TextStyle(fontSize: 12, color: Colors.grey),
                      ),
                    ],
                  ),
                ),
                SizedBox(
                  width: 150,
                  child: ElevatedButton(
                    onPressed: _isLoading ? null : _placeOrder,
                    style: ElevatedButton.styleFrom(backgroundColor: Colors.green),
                    child: _isLoading
                        ? const CircularProgressIndicator(color: Colors.white)
                        : const Text('Place Order'),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _placeOrder() async {
    if (_deliveryAddress.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Please enter delivery address')),
      );
      return;
    }
    setState(() => _isLoading = true);
    try {
      // Place order API call
      await Future.delayed(const Duration(seconds: 1));
      ref.read(cartProvider.notifier).clearCart();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Order placed successfully!')),
        );
        Navigator.popUntil(context, (route) => route.isFirst);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error: ${e.toString()}')),
        );
      }
    } finally {
      if (mounted) setState(() => _isLoading = false);
    }
  }
}

class _CartItemRow extends StatelessWidget {
  final CartItem item;
  const _CartItemRow({required this.item});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(item.name, style: const TextStyle(fontWeight: FontWeight.bold)),
                Text('Quantity: ${item.quantity}', style: const TextStyle(fontSize: 12, color: Colors.grey)),
              ],
            ),
          ),
          Text('£${(item.price * item.quantity).toStringAsFixed(2)}', style: const TextStyle(fontWeight: FontWeight.bold)),
        ],
      ),
    );
  }
}

class _OrderSummaryRow extends StatelessWidget {
  final String label;
  final double amount;
  final bool isBold;

  const _OrderSummaryRow({required this.label, required this.amount, this.isBold = false});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(label, style: TextStyle(fontWeight: isBold ? FontWeight.bold : FontWeight.normal)),
          Text(
            '£${amount.toStringAsFixed(2)}',
            style: TextStyle(fontWeight: isBold ? FontWeight.bold : FontWeight.normal),
          ),
        ],
      ),
    );
  }
}

class _PaymentMethodCard extends StatelessWidget {
  final String title;
  final IconData icon;
  final bool isSelected;
  final VoidCallback onTap;

  const _PaymentMethodCard({
    required this.title,
    required this.icon,
    required this.isSelected,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(vertical: 12),
        decoration: BoxDecoration(
          color: isSelected ? Colors.green.withOpacity(0.1) : Colors.grey[100],
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: isSelected ? Colors.green : Colors.transparent),
        ),
        child: Column(
          children: [
            Icon(icon, color: isSelected ? Colors.green : Colors.grey),
            const SizedBox(height: 4),
            Text(title, style: TextStyle(color: isSelected ? Colors.green : Colors.grey)),
          ],
        ),
      ),
    );
  }
}