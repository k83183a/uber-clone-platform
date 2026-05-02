import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/payment_provider.dart';
import '../widgets/add_payment_method_dialog.dart';

class PaymentMethodsPage extends ConsumerStatefulWidget {
  const PaymentMethodsPage({super.key});

  @override
  ConsumerState<PaymentMethodsPage> createState() => _PaymentMethodsPageState();
}

class _PaymentMethodsPageState extends ConsumerState<PaymentMethodsPage> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(paymentMethodsProvider.notifier).loadMethods();
    });
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(paymentMethodsProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Payment Methods')),
      body: state.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        loaded: (methods) => ListView.builder(
          padding: const EdgeInsets.all(16),
          itemCount: methods.length + 1,
          itemBuilder: (context, index) {
            if (index == methods.length) {
              return _AddPaymentMethodCard(
                onTap: () => showDialog(
                  context: context,
                  builder: (_) => const AddPaymentMethodDialog(),
                ),
              );
            }
            final method = methods[index];
            return _PaymentMethodCard(
              method: method,
              isDefault: method.isDefault,
              onSetDefault: () => ref.read(paymentMethodsProvider.notifier).setDefault(method.id),
              onDelete: () => ref.read(paymentMethodsProvider.notifier).deleteMethod(method.id),
            );
          },
        ),
        error: (error) => Center(child: Text('Error: $error')),
      ),
    );
  }
}

class _PaymentMethodCard extends StatelessWidget {
  final PaymentMethod method;
  final bool isDefault;
  final VoidCallback onSetDefault;
  final VoidCallback onDelete;

  const _PaymentMethodCard({
    required this.method,
    required this.isDefault,
    required this.onSetDefault,
    required this.onDelete,
  });

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: ListTile(
        leading: Container(
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: _getIconColor().withOpacity(0.1),
            shape: BoxShape.circle,
          ),
          child: Icon(_getIcon(), color: _getIconColor()),
        ),
        title: Text(_getTitle()),
        subtitle: Text(_getSubtitle()),
        trailing: PopupMenuButton<String>(
          onSelected: (value) {
            if (value == 'default') onSetDefault();
            if (value == 'delete') onDelete();
          },
          itemBuilder: (context) => [
            const PopupMenuItem(value: 'default', child: Text('Set as default')),
            const PopupMenuItem(value: 'delete', child: Text('Delete', style: TextStyle(color: Colors.red))),
          ],
        ),
        tileColor: isDefault ? Colors.green.withOpacity(0.05) : null,
      ),
    );
  }

  IconData _getIcon() {
    switch (method.methodType) {
      case 'card':
        return Icons.credit_card;
      case 'apple_pay':
        return Icons.apple;
      case 'google_pay':
        return Icons.android;
      case 'paypal':
        return Icons.payment;
      default:
        return Icons.credit_card;
    }
  }

  Color _getIconColor() {
    switch (method.methodType) {
      case 'card':
        return Colors.blue;
      case 'apple_pay':
        return Colors.black;
      case 'google_pay':
        return Colors.green;
      case 'paypal':
        return Colors.blue;
      default:
        return Colors.grey;
    }
  }

  String _getTitle() {
    if (method.methodType == 'card') {
      return '${method.cardBrand?.toUpperCase()} •••• ${method.lastFour}';
    }
    return method.methodType.toUpperCase().replaceAll('_', ' ');
  }

  String _getSubtitle() {
    if (method.methodType == 'card') {
      return 'Expires ${method.expiryMonth}/${method.expiryYear}';
    }
    if (isDefault) return 'Default payment method';
    return 'Tap to set as default';
  }
}

class _AddPaymentMethodCard extends StatelessWidget {
  final VoidCallback onTap;
  const _AddPaymentMethodCard({required this.onTap});

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: ListTile(
        leading: Container(
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: Colors.green.withOpacity(0.1),
            shape: BoxShape.circle,
          ),
          child: const Icon(Icons.add, color: Colors.green),
        ),
        title: const Text('Add Payment Method'),
        subtitle: const Text('Credit card, Apple Pay, Google Pay, PayPal'),
        onTap: onTap,
      ),
    );
  }
}