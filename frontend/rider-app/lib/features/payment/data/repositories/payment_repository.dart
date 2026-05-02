import 'package:dio/dio.dart';
import 'package:riverpod/riverpod.dart';
import '../../../../core/network/dio_client.dart';
import '../models/payment_method.dart';

final paymentRepositoryProvider = Provider((ref) {
  final dio = ref.read(dioProvider);
  return PaymentRepository(dio);
});

class PaymentRepository {
  final Dio _dio;

  PaymentRepository(this._dio);

  Future<List<PaymentMethod>> getPaymentMethods() async {
    final response = await _dio.get('/api/v1/payment/methods');
    return (response.data['methods'] as List)
        .map((json) => PaymentMethod.fromJson(json))
        .toList();
  }

  Future<PaymentMethod> addPaymentMethod(PaymentMethod method) async {
    final response = await _dio.post('/api/v1/payment/methods', data: {
      'method_type': method.methodType,
      'stripe_payment_method_id': method.id,
      'set_default': true,
    });
    return PaymentMethod.fromJson(response.data);
  }

  Future<void> setDefaultMethod(String methodId) async {
    await _dio.put('/api/v1/payment/methods/$methodId/default');
  }

  Future<void> deletePaymentMethod(String methodId) async {
    await _dio.delete('/api/v1/payment/methods/$methodId');
  }
}