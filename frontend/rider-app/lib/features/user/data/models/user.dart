class User {
  final String id;
  final String userId;
  final String email;
  final String fullName;
  final String phone;
  final String? avatarUrl;
  final String role;
  final bool isActive;
  final String createdAt;

  User({
    required this.id,
    required this.userId,
    required this.email,
    required this.fullName,
    required this.phone,
    this.avatarUrl,
    required this.role,
    required this.isActive,
    required this.createdAt,
  });

  factory User.fromJson(Map<String, dynamic> json) {
    return User(
      id: json['id'],
      userId: json['user_id'],
      email: json['email'],
      fullName: json['full_name'],
      phone: json['phone'],
      avatarUrl: json['avatar_url'],
      role: json['role'],
      isActive: json['is_active'] ?? true,
      createdAt: json['created_at'],
    );
  }
}