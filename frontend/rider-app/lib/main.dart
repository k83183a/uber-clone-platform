import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:go_router/go_router.dart';
import 'package:dio/dio.dart';
import 'core/network/dio_client.dart';
import 'core/utils/logger.dart';
import 'features/auth/presentation/pages/login_page.dart';
import 'features/auth/presentation/pages/register_page.dart';
import 'features/home/presentation/pages/home_page.dart';
import 'features/ride/presentation/pages/ride_request_page.dart';
import 'features/ride/presentation/pages/ride_tracking_page.dart';
import 'features/food/presentation/pages/food_page.dart';
import 'features/grocery/presentation/pages/grocery_page.dart';
import 'features/courier/presentation/pages/courier_page.dart';
import 'features/profile/presentation/pages/profile_page.dart';
import 'features/payment/presentation/pages/payment_methods_page.dart';
import 'features/loyalty/presentation/pages/loyalty_page.dart';
import 'features/promotions/presentation/pages/promotions_page.dart';
import 'features/subscription/presentation/pages/subscription_page.dart';

final storage = const FlutterSecureStorage();

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final token = await storage.read(key: 'access_token');
  runApp(ProviderScope(child: RiderApp(initialToken: token)));
}

class RiderApp extends StatelessWidget {
  final String? initialToken;
  const RiderApp({super.key, this.initialToken});

  @override
  Widget build(BuildContext context) {
    return MaterialApp.router(
      title: 'Prosser Rider',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        primarySwatch: Colors.green,
        fontFamily: 'Roboto',
        appBarTheme: const AppBarTheme(
          backgroundColor: Colors.green,
          foregroundColor: Colors.white,
          elevation: 0,
        ),
        elevatedButtonTheme: ElevatedButtonThemeData(
          style: ElevatedButton.styleFrom(
            backgroundColor: Colors.green,
            foregroundColor: Colors.white,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(12),
            ),
            padding: const EdgeInsets.symmetric(vertical: 16),
          ),
        ),
      ),
      routerConfig: _router,
    );
  }

  late final GoRouter _router = GoRouter(
    initialLocation: initialToken != null ? '/home' : '/login',
    routes: [
      GoRoute(
        path: '/login',
        name: 'login',
        builder: (_, __) => const LoginPage(),
      ),
      GoRoute(
        path: '/register',
        name: 'register',
        builder: (_, __) => const RegisterPage(),
      ),
      GoRoute(
        path: '/home',
        name: 'home',
        builder: (_, __) => const HomePage(),
      ),
      GoRoute(
        path: '/ride',
        name: 'ride',
        builder: (_, __) => const RideRequestPage(),
      ),
      GoRoute(
        path: '/ride/tracking/:rideId',
        name: 'ride_tracking',
        builder: (_, state) => RideTrackingPage(
          rideId: state.pathParameters['rideId']!,
        ),
      ),
      GoRoute(
        path: '/food',
        name: 'food',
        builder: (_, __) => const FoodPage(),
      ),
      GoRoute(
        path: '/grocery',
        name: 'grocery',
        builder: (_, __) => const GroceryPage(),
      ),
      GoRoute(
        path: '/courier',
        name: 'courier',
        builder: (_, __) => const CourierPage(),
      ),
      GoRoute(
        path: '/profile',
        name: 'profile',
        builder: (_, __) => const ProfilePage(),
      ),
      GoRoute(
        path: '/payment/methods',
        name: 'payment_methods',
        builder: (_, __) => const PaymentMethodsPage(),
      ),
      GoRoute(
        path: '/loyalty',
        name: 'loyalty',
        builder: (_, __) => const LoyaltyPage(),
      ),
      GoRoute(
        path: '/promotions',
        name: 'promotions',
        builder: (_, __) => const PromotionsPage(),
      ),
      GoRoute(
        path: '/subscription',
        name: 'subscription',
        builder: (_, __) => const SubscriptionPage(),
      ),
    ],
  );
}