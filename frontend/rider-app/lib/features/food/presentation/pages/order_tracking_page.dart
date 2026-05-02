import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

class OrderTrackingPage extends ConsumerStatefulWidget {
  final String orderId;
  const OrderTrackingPage({super.key, required this.orderId});

  @override
  ConsumerState<OrderTrackingPage> createState() => _OrderTrackingPageState();
}

class _OrderTrackingPageState extends ConsumerState<OrderTrackingPage> {
  String _status = 'confirmed';
  int _currentStep = 1;
  final int _totalSteps = 6;

  final List<OrderStatusStep> _statusSteps = [
    OrderStatusStep(status: 'confirmed', title: 'Order Confirmed', icon: Icons.check_circle),
    OrderStatusStep(status: 'preparing', title: 'Restaurant Preparing', icon: Icons.restaurant),
    OrderStatusStep(status: 'ready', title: 'Ready for Pickup', icon: Icons.kitchen),
    OrderStatusStep(status: 'out_for_delivery', title: 'Out for Delivery', icon: Icons.delivery_dining),
    OrderStatusStep(status: 'nearby', title: 'Driver Nearby', icon: Icons.location_on),
    OrderStatusStep(status: 'delivered', title: 'Delivered', icon: Icons.check_circle),
  ];

  @override
  void initState() {
    super.initState();
    _simulateOrderProgress();
  }

  void _simulateOrderProgress() {
    Future.delayed(const Duration(seconds: 10), () {
      if (mounted) setState(() {
        _status = 'preparing';
        _currentStep = 2;
      });
    });
    Future.delayed(const Duration(seconds: 25), () {
      if (mounted) setState(() {
        _status = 'ready';
        _currentStep = 3;
      });
    });
    Future.delayed(const Duration(seconds: 35), () {
      if (mounted) setState(() {
        _status = 'out_for_delivery';
        _currentStep = 4;
      });
    });
    Future.delayed(const Duration(seconds: 45), () {
      if (mounted) setState(() {
        _status = 'nearby';
        _currentStep = 5;
      });
    });
    Future.delayed(const Duration(seconds: 55), () {
      if (mounted) setState(() {
        _status = 'delivered';
        _currentStep = 6;
      });
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Order Tracking')),
      body: Column(
        children: [
          Container(
            height: 200,
            width: double.infinity,
            color: Colors.grey[200],
            child: const Center(child: Text('Live Map View - Driver Location')),
          ),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      children: [
                        Row(
                          children: [
                            const Icon(Icons.receipt, color: Colors.green),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  const Text('Order #FOOD202405011234'),
                                  Text('Total: £24.50', style: const TextStyle(fontWeight: FontWeight.bold)),
                                ],
                              ),
                            ),
                            Container(
                              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
                              decoration: BoxDecoration(
                                color: _getStatusColor().withOpacity(0.1),
                                borderRadius: BorderRadius.circular(12),
                              ),
                              child: Text(
                                _status.toUpperCase().replaceAll('_', ' '),
                                style: TextStyle(color: _getStatusColor()),
                              ),
                            ),
                          ],
                        ),
                        const SizedBox(height: 16),
                        const Divider(),
                        const SizedBox(height: 16),
                        ..._buildStepper(),
                      ],
                    ),
                  ),
                ),
                const SizedBox(height: 16),
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text('Delivery Address', style: TextStyle(fontWeight: FontWeight.bold)),
                        const SizedBox(height: 4),
                        const Text('123 High Street, London, SW1A 1AA'),
                        const SizedBox(height: 8),
                        const Divider(),
                        const SizedBox(height: 8),
                        Row(
                          children: [
                            const CircleAvatar(
                              backgroundColor: Colors.green,
                              radius: 16,
                              child: Icon(Icons.person, size: 16, color: Colors.white),
                            ),
                            const SizedBox(width: 8),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  const Text('Delivery Driver', style: TextStyle(fontWeight: FontWeight.bold)),
                                  Text('Arriving in 15 min', style: TextStyle(color: Colors.green)),
                                ],
                              ),
                            ),
                            IconButton(icon: const Icon(Icons.phone), onPressed: () {}),
                            IconButton(icon: const Icon(Icons.chat), onPressed: () {}),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Color _getStatusColor() {
    switch (_status) {
      case 'confirmed': return Colors.blue;
      case 'preparing': return Colors.orange;
      case 'ready': return Colors.purple;
      case 'out_for_delivery': return Colors.green;
      case 'nearby': return Colors.teal;
      case 'delivered': return Colors.green;
      default: return Colors.grey;
    }
  }

  List<Widget> _buildStepper() {
    return _statusSteps.asMap().entries.map((entry) {
      final index = entry.key;
      final step = entry.value;
      final isCompleted = index + 1 <= _currentStep;
      final isCurrent = index + 1 == _currentStep;

      return Row(
        children: [
          Column(
            children: [
              Container(
                width: 32,
                height: 32,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: isCompleted ? Colors.green : Colors.grey[300],
                ),
                child: Icon(
                  isCompleted ? Icons.check : step.icon,
                  size: 18,
                  color: Colors.white,
                ),
              ),
              if (index < _statusSteps.length - 1)
                Container(
                  width: 2,
                  height: 40,
                  color: _currentStep > index + 1 ? Colors.green : Colors.grey[300],
                ),
            ],
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  step.title,
                  style: TextStyle(
                    fontWeight: isCurrent ? FontWeight.bold : FontWeight.normal,
                    color: isCompleted ? Colors.green : Colors.grey,
                  ),
                ),
                if (isCurrent)
                  Text(
                    'Estimated time: ${_getEstimatedTime()}',
                    style: TextStyle(fontSize: 12, color: Colors.grey[600]),
                  ),
              ],
            ),
          ),
        ],
      );
    }).toList();
  }

  String _getEstimatedTime() {
    switch (_status) {
      case 'confirmed': return '5-10 min';
      case 'preparing': return '15-20 min';
      case 'ready': return '25-30 min';
      case 'out_for_delivery': return '35-40 min';
      case 'nearby': return '5 min';
      default: return '--';
    }
  }
}

class OrderStatusStep {
  final String status;
  final String title;
  final IconData icon;

  OrderStatusStep({required this.status, required this.title, required this.icon});
}