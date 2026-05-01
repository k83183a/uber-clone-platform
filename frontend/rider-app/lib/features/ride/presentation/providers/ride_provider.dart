import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../data/repositories/ride_repository.dart';
import '../../data/models/ride.dart';

final rideProvider = StateNotifierProvider<RideNotifier, RideState>((ref) {
  final repo = ref.read(rideRepositoryProvider);
  return RideNotifier(repo);
});

class RideNotifier extends StateNotifier<RideState> {
  final RideRepository _repository;

  RideNotifier(this._repository) : super(const RideState.initial());

  Future<Ride> requestRide({
    required String riderId,
    required double pickupLat,
    required double pickupLng,
    required String pickupAddress,
    required double dropoffLat,
    required double dropoffLng,
    required String dropoffAddress,
    required String rideType,
    required double fareEstimate,
    required String paymentMethod,
  }) async {
    state = const RideState.loading();
    try {
      final ride = await _repository.requestRide(
        riderId: riderId,
        pickupLat: pickupLat,
        pickupLng: pickupLng,
        pickupAddress: pickupAddress,
        dropoffLat: dropoffLat,
        dropoffLng: dropoffLng,
        dropoffAddress: dropoffAddress,
        rideType: rideType,
        fareEstimate: fareEstimate,
        paymentMethod: paymentMethod,
      );
      state = RideState.loaded(ride);
      return ride;
    } catch (e) {
      state = RideState.error(e.toString());
      rethrow;
    }
  }

  Future<Ride> getRide(String rideId) async {
    final ride = await _repository.getRide(rideId);
    state = RideState.loaded(ride);
    return ride;
  }

  Future<void> cancelRide(String rideId, String reason) async {
    await _repository.cancelRide(rideId, reason);
    state = const RideState.initial();
  }
}

class RideState {
  final bool isLoading;
  final Ride? ride;
  final String? error;

  const RideState._({required this.isLoading, this.ride, this.error});

  const RideState.initial() : this._(isLoading: false);
  const RideState.loading() : this._(isLoading: true);
  const RideState.loaded(Ride ride) : this._(isLoading: false, ride: ride);
  const RideState.error(String error) : this._(isLoading: false, error: error);
}