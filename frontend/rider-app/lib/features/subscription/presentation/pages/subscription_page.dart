import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/subscription_provider.dart';

class SubscriptionPage extends ConsumerStatefulWidget {
  const SubscriptionPage({super.key});

  @override
  ConsumerState<SubscriptionPage> createState() => _SubscriptionPageState();
}

class _SubscriptionPageState extends ConsumerState<SubscriptionPage> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(subscriptionProvider.notifier).loadPlans();
      ref.read(userSubscriptionProvider.notifier).loadSubscription();
    });
  }

  @override
  Widget build(BuildContext context) {
    final plansState = ref.watch(subscriptionProvider);
    final userSubState = ref.watch(userSubscriptionProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Uber Pass')),
      body: SingleChildScrollView(
        child: Column(
          children: [
            userSubState.when(
              loading: () => const SizedBox(),
              loaded: (subscription) {
                if (subscription != null && subscription.status == 'active') {
                  return _ActiveSubscriptionCard(subscription: subscription);
                }
                return const SizedBox();
              },
              error: (error) => const SizedBox(),
            ),
            const SizedBox(height: 16),
            Container(
              margin: const EdgeInsets.all(16),
              padding: const EdgeInsets.all(20),
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  colors: [Colors.green.shade800, Colors.green.shade500],
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                ),
                borderRadius: BorderRadius.circular(20),
              ),
              child: Column(
                children: [
                  const Icon(Icons.star, color: Colors.white, size: 40),
                  const SizedBox(height: 8),
                  const Text(
                    'Save 10% on every ride',
                    style: TextStyle(color: Colors.white, fontSize: 18),
                  ),
                  const SizedBox(height: 4),
                  const Text(
                    'Plus free delivery on food & groceries',
                    style: TextStyle(color: Colors.white),
                  ),
                  const SizedBox(height: 16),
                  userSubState.when(
                    loading: () => const SizedBox(),
                    loaded: (subscription) {
                      if (subscription != null && subscription.status == 'active') {
                        return ElevatedButton(
                          onPressed: () => ref.read(userSubscriptionProvider.notifier).cancelSubscription(),
                          style: ElevatedButton.styleFrom(
                            backgroundColor: Colors.white,
                            foregroundColor: Colors.green,
                          ),
                          child: const Text('Cancel Subscription'),
                        );
                      }
                      return ElevatedButton(
                        onPressed: () => _showSubscribeDialog(),
                        style: ElevatedButton.styleFrom(
                          backgroundColor: Colors.white,
                          foregroundColor: Colors.green,
                        ),
                        child: const Text('Subscribe Now - £9.99/month'),
                      );
                    },
                    error: (error) => const Text('Error loading subscription'),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 24),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text(
                    'Benefits',
                    style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                  ),
                  const SizedBox(height: 12),
                  _BenefitItem(icon: Icons.discount, title: '10% off all rides'),
                  _BenefitItem(icon: Icons.delivery_dining, title: 'Free delivery on food orders (min £10)'),
                  _BenefitItem(icon: Icons.shopping_cart, title: 'Free delivery on groceries (min £15)'),
                  _BenefitItem(icon: Icons.support_agent, title: 'Priority customer support'),
                  _BenefitItem(icon: Icons.verified, title: 'Exclusive member-only promotions'),
                ],
              ),
            ),
            const SizedBox(height: 24),
            plansState.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              loaded: (plans) => ListView.builder(
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                padding: const EdgeInsets.symmetric(horizontal: 16),
                itemCount: plans.length,
                itemBuilder: (context, index) {
                  final plan = plans[index];
                  return _PlanCard(
                    plan: plan,
                    isSelected: userSubState.valueOrNull?.planId == plan.id,
                    onSubscribe: () => _showSubscribeDialog(plan: plan),
                  );
                },
              ),
              error: (error) => Center(child: Text('Error: $error')),
            ),
            const SizedBox(height: 32),
          ],
        ),
      ),
    );
  }

  void _showSubscribeDialog({Plan? plan}) {
    showDialog(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Subscribe to Uber Pass'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('Get 10% off all rides and free delivery for £9.99/month.'),
            const SizedBox(height: 16),
            const Text('Your subscription will auto-renew monthly.'),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Cancel'),
          ),
          ElevatedButton(
            onPressed: () async {
              Navigator.pop(context);
              await ref.read(userSubscriptionProvider.notifier).createSubscription(plan?.id ?? 'monthly');
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(content: Text('Subscription activated!')),
              );
            },
            style: ElevatedButton.styleFrom(backgroundColor: Colors.green),
            child: const Text('Subscribe'),
          ),
        ],
      ),
    );
  }
}

class _ActiveSubscriptionCard extends StatelessWidget {
  final Subscription subscription;
  const _ActiveSubscriptionCard({required this.subscription});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.all(16),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.green[50],
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Colors.green[200]!),
      ),
      child: Row(
        children: [
          const Icon(Icons.check_circle, color: Colors.green, size: 32),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Active Subscription',
                  style: TextStyle(fontWeight: FontWeight.bold, color: Colors.green[800]),
                ),
                Text(
                  '${subscription.planName} plan • ${subscription.discountPercent}% off rides',
                  style: const TextStyle(fontSize: 12),
                ),
                Text(
                  'Renews on ${DateTime.fromMillisecondsSinceEpoch(subscription.endDate).toLocal().toString().substring(0, 10)}',
                  style: const TextStyle(fontSize: 12, color: Colors.grey),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _BenefitItem extends StatelessWidget {
  final IconData icon;
  final String title;
  const _BenefitItem({required this.icon, required this.title});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: [
          Icon(icon, color: Colors.green, size: 24),
          const SizedBox(width: 12),
          Expanded(child: Text(title, style: const TextStyle(fontSize: 16))),
        ],
      ),
    );
  }
}

class _PlanCard extends StatelessWidget {
  final Plan plan;
  final bool isSelected;
  final VoidCallback onSubscribe;

  const _PlanCard({required this.plan, required this.isSelected, required this.onSubscribe});

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      color: isSelected ? Colors.green.withOpacity(0.05) : null,
      child: ListTile(
        leading: CircleAvatar(
          backgroundColor: isSelected ? Colors.green : Colors.grey[200],
          child: Icon(
            plan.name == 'Monthly Pass' ? Icons.calendar_month : Icons.calendar_today,
            color: isSelected ? Colors.white : Colors.grey,
          ),
        ),
        title: Text(plan.name),
        subtitle: Text('£${plan.priceGbp.toStringAsFixed(2)}/${plan.billingPeriod}'),
        trailing: isSelected
            ? const Chip(label: Text('Active'), backgroundColor: Colors.green)
            : ElevatedButton(
                onPressed: onSubscribe,
                style: ElevatedButton.styleFrom(backgroundColor: Colors.green),
                child: const Text('Subscribe'),
              ),
      ),
    );
  }
}