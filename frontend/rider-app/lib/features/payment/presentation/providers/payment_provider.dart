import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../data/repositories/payment_repository.dart';
import '../../data/models/payment_method.dart';

final paymentMethodsProvider = StateNotifierProvider<PaymentMethodsNotifier, PaymentMethodsState>((ref) {
  final repo = ref.read(paymentRepositoryProvider);
  return PaymentMethodsNotifier(repo);
});

class PaymentMethodsNotifier extends StateNotifier<PaymentMethodsState> {
  final PaymentRepository _repository;

  PaymentMethodsNotifier(this._repository) : super(const PaymentMethodsState.initial());

  Future<void> loadMethods() async {
    state = const PaymentMethodsState.loading();
    try {
      final methods = await _repository.getPaymentMethods();
      state = PaymentMethodsState.loaded(methods);
    } catch (e) {
      state = PaymentMethodsState.error(e.toString());
    }
  }

  Future<void> addMethod(PaymentMethod method) async {
    try {
      final newMethod = await _repository.addPaymentMethod(method);
      final currentMethods = state.methods ?? [];
      state = PaymentMethodsState.loaded([...currentMethods, newMethod]);
    } catch (e) {
      state = PaymentMethodsState.error(e.toString());
    }
  }

  Future<void> setDefault(String methodId) async {
    await _repository.setDefaultMethod(methodId);
    await loadMethods();
  }

  Future<void> deleteMethod(String methodId) async {
    await _repository.deletePaymentMethod(methodId);
    await loadMethods();
  }
}

class PaymentMethodsState {
  final bool isLoading;
  final List<PaymentMethod>? methods;
  final String? error;

  const PaymentMethodsState._({required this.isLoading, this.methods, this.error});

  const PaymentMethodsState.initial() : this._(isLoading: false);
  const PaymentMethodsState.loading() : this._(isLoading: true);
  const PaymentMethodsState.loaded(List<PaymentMethod> methods) : this._(isLoading: false, methods: methods);
  const PaymentMethodsState.error(String error) : this._(isLoading: false, error: error);
}