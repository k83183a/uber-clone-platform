import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/promotions_provider.dart';

class PromotionsPage extends ConsumerStatefulWidget {
  const PromotionsPage({super.key});

  @override
  ConsumerState<PromotionsPage> createState() => _PromotionsPageState();
}

class _PromotionsPageState extends ConsumerState<PromotionsPage> {
  final TextEditingController _promoController = TextEditingController();
  String _appliedCode = '';

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      ref.read(promotionsProvider.notifier).loadPromotions();
      ref.read(aiPromotionsProvider.notifier).loadAIPromotions();
    });
  }

  @override
  Widget build(BuildContext context) {
    final promotionsState = ref.watch(promotionsProvider);
    final aiPromotionsState = ref.watch(aiPromotionsProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Promotions & Offers')),
      body: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              margin: const EdgeInsets.all(16),
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              decoration: BoxDecoration(
                color: Colors.green.withOpacity(0.1),
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: Colors.green.withOpacity(0.3)),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _promoController,
                      decoration: const InputDecoration(
                        hintText: 'Enter promo code',
                        border: InputBorder.none,
                      ),
                    ),
                  ),
                  ElevatedButton(
                    onPressed: _appliedCode == _promoController.text.trim()
                        ? null
                        : () {
                            setState(() {
                              _appliedCode = _promoController.text.trim();
                            });
                            ref.read(promotionsProvider.notifier).applyPromoCode(_appliedCode);
                          },
                    style: ElevatedButton.styleFrom(
                      backgroundColor: Colors.green,
                      foregroundColor: Colors.white,
                    ),
                    child: const Text('Apply'),
                  ),
                ],
              ),
            ),
            if (_appliedCode.isNotEmpty)
              Container(
                margin: const EdgeInsets.symmetric(horizontal: 16),
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: Colors.green[50],
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Row(
                  children: [
                    const Icon(Icons.check_circle, color: Colors.green),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text('Promo code "$_appliedCode" applied!'),
                    ),
                    IconButton(
                      icon: const Icon(Icons.close, size: 16),
                      onPressed: () {
                        setState(() => _appliedCode = '');
                        ref.read(promotionsProvider.notifier).removePromoCode();
                      },
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Text(
                'AI Recommended for You',
                style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                  color: Colors.purple[700],
                ),
              ),
            ),
            aiPromotionsState.when(
              loading: () => const Padding(
                padding: EdgeInsets.all(16),
                child: Center(child: CircularProgressIndicator()),
              ),
              loaded: (promotions) => promotions.isEmpty
                  ? const Padding(
                      padding: EdgeInsets.all(16),
                      child: Text('No personalized offers yet'),
                    )
                  : SizedBox(
                      height: 160,
                      child: ListView.builder(
                        scrollDirection: Axis.horizontal,
                        padding: const EdgeInsets.symmetric(horizontal: 16),
                        itemCount: promotions.length,
                        itemBuilder: (context, index) {
                          final promo = promotions[index];
                          return _PromoCard(
                            promotion: promo,
                            isAIPromo: true,
                            onApply: () {
                              setState(() => _appliedCode = promo.code);
                              ref.read(promotionsProvider.notifier).applyPromoCode(promo.code);
                            },
                          );
                        },
                      ),
                    ),
              error: (error) => Padding(
                padding: const EdgeInsets.all(16),
                child: Text('Error: $error'),
              ),
            ),
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: const Text(
                'All Promotions',
                style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
              ),
            ),
            promotionsState.when(
              loading: () => const Padding(
                padding: EdgeInsets.all(16),
                child: Center(child: CircularProgressIndicator()),
              ),
              loaded: (promotions) => promotions.isEmpty
                  ? const Padding(
                      padding: EdgeInsets.all(16),
                      child: Text('No promotions available'),
                    )
                  : ListView.builder(
                      shrinkWrap: true,
                      physics: const NeverScrollableScrollPhysics(),
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                      itemCount: promotions.length,
                      itemBuilder: (context, index) {
                        final promo = promotions[index];
                        return _PromoCard(
                          promotion: promo,
                          isAIPromo: false,
                          onApply: () {
                            setState(() => _appliedCode = promo.code);
                            ref.read(promotionsProvider.notifier).applyPromoCode(promo.code);
                          },
                        );
                      },
                    ),
              error: (error) => Padding(
                padding: const EdgeInsets.all(16),
                child: Text('Error: $error'),
              ),
            ),
            const SizedBox(height: 20),
          ],
        ),
      ),
    );
  }
}

class _PromoCard extends StatelessWidget {
  final Promotion promotion;
  final bool isAIPromo;
  final VoidCallback onApply;

  const _PromoCard({
    required this.promotion,
    required this.isAIPromo,
    required this.onApply,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 280,
      margin: const EdgeInsets.only(right: 12, bottom: 12),
      decoration: BoxDecoration(
        gradient: isAIPromo
            ? LinearGradient(
                colors: [Colors.purple.shade50, Colors.pink.shade50],
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
              )
            : null,
        color: isAIPromo ? null : Colors.white,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Colors.grey.shade200),
        boxShadow: [
          BoxShadow(
            color: Colors.grey.withOpacity(0.1),
            blurRadius: 4,
            offset: const Offset(0, 2),
          ),
        ],
      ),
      child: Stack(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (isAIPromo) ...[
                  Row(
                    children: [
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
                        decoration: BoxDecoration(
                          color: Colors.purple,
                          borderRadius: BorderRadius.circular(12),
                        ),
                        child: const Text(
                          'AI RECOMMENDED',
                          style: TextStyle(color: Colors.white, fontSize: 10),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                ],
                Row(
                  children: [
                    Container(
                      padding: const EdgeInsets.all(8),
                      decoration: BoxDecoration(
                        color: (isAIPromo ? Colors.purple : Colors.green).withOpacity(0.1),
                        shape: BoxShape.circle,
                      ),
                      child: Icon(
                        Icons.local_offer,
                        color: isAIPromo ? Colors.purple : Colors.green,
                        size: 20,
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            promotion.name,
                            style: const TextStyle(fontWeight: FontWeight.bold),
                          ),
                          Text(
                            promotion.description,
                            style: TextStyle(fontSize: 12, color: Colors.grey[600]),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                      decoration: BoxDecoration(
                        color: Colors.grey[200],
                        borderRadius: BorderRadius.circular(8),
                      ),
                      child: Text(
                        promotion.code,
                        style: TextStyle(
                          fontWeight: FontWeight.bold,
                          color: isAIPromo ? Colors.purple : Colors.green,
                        ),
                      ),
                    ),
                    const SizedBox(width: 8),
                    if (promotion.minOrderAmount > 0)
                      Text(
                        'Min. £${promotion.minOrderAmount.toStringAsFixed(2)}',
                        style: TextStyle(fontSize: 11, color: Colors.grey[600]),
                      ),
                    const Spacer(),
                    TextButton(
                      onPressed: onApply,
                      style: TextButton.styleFrom(
                        foregroundColor: isAIPromo ? Colors.purple : Colors.green,
                      ),
                      child: const Text('Apply'),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}