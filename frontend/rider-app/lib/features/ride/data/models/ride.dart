class Ride {
  final String id;
  final String riderId;
  final String? driverId;
  final String status;
  final double pickupLat;
  final double pickupLng;
  final String pickupAddress;
  final double dropoffLat;
  final double dropoffLng;
  final String dropoffAddress;
  final String rideType;
  final double fare;
  final String createdAt;

  Ride({
    required this.id,
    required this.riderId,
    this.driverId,
    required this.status,
    required this.pickupLat,
    required this.pickupLng,
    required this.pickupAddress,
    required this.dropoffLat,
    required this.dropoffLng,
    required this.dropoffAddress,
    required this.rideType,
    required this.fare,
    required this.createdAt,
  });

  factory Ride.fromJson(Map<String, dynamic> json) {
    return Ride(
      id: json['id'],
      riderId: json['rider_id'],
      driverId: json['driver_id'],
      status: json['status'],
      pickupLat: json['pickup_lat'].toDouble(),
      pickupLng: json['pickup_lng'].toDouble(),
      pickupAddress: json['pickup_address'],
      dropoffLat: json['dropoff_lat'].toDouble(),
      dropoffLng: json['dropoff_lng'].toDouble(),
      dropoffAddress: json['dropoff_address'],
      rideType: json['ride_type'],
      fare: json['fare'].toDouble(),
      createdAt: json['created_at'],
    );
  }
}