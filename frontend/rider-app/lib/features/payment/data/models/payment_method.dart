class PaymentMethod {
  final String id;
  final String methodType;
  final String? lastFour;
  final String? cardBrand;
  final int? expiryMonth;
  final int? expiryYear;
  final bool isDefault;

  PaymentMethod({
    required this.id,
    required this.methodType,
    this.lastFour,
    this.cardBrand,
    this.expiryMonth,
    this.expiryYear,
    required this.isDefault,
  });

  factory PaymentMethod.fromJson(Map<String, dynamic> json) {
    return PaymentMethod(
      id: json['id'],
      methodType: json['method_type'],
      lastFour: json['last_four'],
      cardBrand: json['card_brand'],
      expiryMonth: json['expiry_month'],
      expiryYear: json['expiry_year'],
      isDefault: json['is_default'],
    );
  }
}