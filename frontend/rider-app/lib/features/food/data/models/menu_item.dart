class MenuItem {
  final String id;
  final String name;
  final String description;
  final double price;
  final double discountPrice;
  final String imageUrl;
  final String category;
  final bool isVegetarian;
  final bool isVegan;
  final bool isSpicy;
  final int preparationTime;

  MenuItem({
    required this.id,
    required this.name,
    required this.description,
    required this.price,
    required this.discountPrice,
    required this.imageUrl,
    required this.category,
    required this.isVegetarian,
    required this.isVegan,
    required this.isSpicy,
    required this.preparationTime,
  });

  factory MenuItem.fromJson(Map<String, dynamic> json) {
    return MenuItem(
      id: json['id'],
      name: json['name'],
      description: json['description'] ?? '',
      price: json['price'].toDouble(),
      discountPrice: json['discount_price']?.toDouble() ?? 0,
      imageUrl: json['image_url'] ?? '',
      category: json['category'] ?? 'General',
      isVegetarian: json['is_vegetarian'] ?? false,
      isVegan: json['is_vegan'] ?? false,
      isSpicy: json['is_spicy'] ?? false,
      preparationTime: json['preparation_time'] ?? 15,
    );
  }
}