import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/loyalty_provider.dart';

class LoyaltyPage extends ConsumerStatefulWidget {
  const LoyaltyPage({super.key});

  @override
  ConsumerState<LoyaltyPage> createState() => _LoyaltyPageState();
}

class _LoyaltyPageState extends ConsumerState<LoyaltyPage> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(loyaltyProvider.notifier).loadAccount();
    });
  }

  @override
  Widget build(BuildContext context) {
    final accountState = ref.watch(loyaltyProvider);
    final rewardsState = ref.watch(rewardsProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Loyalty Rewards')),
      body: accountState.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        loaded: (account) => Column(
          children: [
            _PointsCard(account: account),
            const SizedBox(height: 16),
            _TierProgress(account: account),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.all(16),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  const Text('Available Rewards', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                  Text('${account.pointsBalance} pts', style: const TextStyle(color: Colors.green)),
                ],
              ),
            ),
            Expanded(
              child: rewardsState.when(
                loading: () => const Center(child: CircularProgressIndicator()),
                loaded: (rewards) => ListView.builder(
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  itemCount: rewards.length,
                  itemBuilder: (context, index) {
                    final reward = rewards[index];
                    return _RewardCard(
                      reward: reward,
                      pointsBalance: account.pointsBalance,
                      onRedeem: () => ref.read(loyaltyProvider.notifier).redeemReward(reward.id),
                    );
                  },
                ),
                error: (error) => Center(child: Text('Error: $error')),
              ),
            ),
          ],
        ),
        error: (error) => Center(child: Text('Error: $error')),
      ),
    );
  }
}

class _PointsCard extends StatelessWidget {
  final LoyaltyAccount account;
  const _PointsCard({required this.account});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.all(16),
      padding: const EdgeInsets.all(24),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          colors: [Colors.green.shade800, Colors.green.shade400],
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
        ),
        borderRadius: BorderRadius.circular(20),
      ),
      child: Column(
        children: [
          const Text('Your Points', style: TextStyle(color: Colors.white, fontSize: 14)),
          const SizedBox(height: 8),
          Text(
            '${account.pointsBalance}',
            style: const TextStyle(color: Colors.white, fontSize: 48, fontWeight: FontWeight.bold),
          ),
          const SizedBox(height: 8),
          Row(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: Colors.white.withOpacity(0.3),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  '${account.tier.toUpperCase()} Tier',
                  style: const TextStyle(color: Colors.white, fontSize: 12),
                ),
              ),
              const SizedBox(width: 8),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                decoration: BoxDecoration(
                  color: Colors.white.withOpacity(0.3),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  '${account.lifetimePoints} lifetime points',
                  style: const TextStyle(color: Colors.white, fontSize: 12),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _TierProgress extends StatelessWidget {
  final LoyaltyAccount account;
  const _TierProgress({required this.account});

  @override
  Widget build(BuildContext context) {
    final progress = account.pointsToNextTier / 5000; // 5000 points for Gold
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 16),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.grey[100],
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text('Next Tier: Gold', style: TextStyle(fontWeight: FontWeight.bold)),
              Text('${account.pointsToNextTier} points to go', style: const TextStyle(fontSize: 12, color: Colors.grey)),
            ],
          ),
          const SizedBox(height: 8),
          LinearProgressIndicator(
            value: progress.clamp(0.0, 1.0),
            backgroundColor: Colors.grey[300],
            color: Colors.amber,
          ),
          const SizedBox(height: 8),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: const [
              Text('Silver', style: TextStyle(fontSize: 12)),
              Text('Gold', style: TextStyle(fontSize: 12)),
            ],
          ),
        ],
      ),
    );
  }
}

class _RewardCard extends StatelessWidget {
  final Reward reward;
  final int pointsBalance;
  final VoidCallback onRedeem;
  const _RewardCard({required this.reward, required this.pointsBalance, required this.onRedeem});

  @override
  Widget build(BuildContext context) {
    final canRedeem = pointsBalance >= reward.pointsCost;
    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      child: ListTile(
        leading: Container(
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: Colors.green.withOpacity(0.1),
            shape: BoxShape.circle,
          ),
          child: Icon(_getIcon(), color: Colors.green),
        ),
        title: Text(reward.name),
        subtitle: Text('${reward.pointsCost} points'),
        trailing: ElevatedButton(
          onPressed: canRedeem ? onRedeem : null,
          style: ElevatedButton.styleFrom(
            backgroundColor: canRedeem ? Colors.green : Colors.grey,
            foregroundColor: Colors.white,
          ),
          child: const Text('Redeem'),
        ),
      ),
    );
  }

  IconData _getIcon() {
    switch (reward.rewardType) {
      case 'discount_voucher':
        return Icons.local_offer;
      case 'free_ride':
        return Icons.directions_car;
      default:
        return Icons.card_giftcard;
    }
  }
}