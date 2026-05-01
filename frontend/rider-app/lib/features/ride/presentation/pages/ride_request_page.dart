import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart';
import 'package:geolocator/geolocator.dart';
import 'package:go_router/go_router.dart';
import '../providers/ride_provider.dart';

class RideRequestPage extends ConsumerStatefulWidget {
  const RideRequestPage({super.key});

  @override
  ConsumerState<RideRequestPage> createState() => _RideRequestPageState();
}

class _RideRequestPageState extends ConsumerState<RideRequestPage> {
  LatLng? _pickupLocation;
  LatLng? _dropoffLocation;
  String? _pickupAddress;
  String? _dropoffAddress;
  String _selectedRideType = 'uberX';
  bool _isLoading = false;

  final List<RideType> _rideTypes = [
    RideType(id: 'uberX', name: 'UberX', icon: Icons.directions_car, price: '£8-12', capacity: '4 seats', luggage: '2 bags'),
    RideType(id: 'uberXL', name: 'UberXL', icon: Icons.directions_car, price: '£15-20', capacity: '6 seats', luggage: '4 bags'),
    RideType(id: 'green', name: 'Green', icon: Icons.eco, price: '£10-15', capacity: '4 seats', luggage: '2 bags', isElectric: true),
    RideType(id: 'pet', name: 'Pet', icon: Icons.pets, price: '£9-13', capacity: '4 seats', luggage: '2 bags'),
    RideType(id: 'access', name: 'Access', icon: Icons.accessible, price: '£11-16', capacity: '4 seats', luggage: '2 bags'),
  ];

  @override
  void initState() {
    super.initState();
    _getCurrentLocation();
  }

  Future<void> _getCurrentLocation() async {
    bool serviceEnabled = await Geolocator.isLocationServiceEnabled();
    if (!serviceEnabled) return;
    LocationPermission permission = await Geolocator.checkPermission();
    if (permission == LocationPermission.denied) {
      permission = await Geolocator.requestPermission();
      if (permission == LocationPermission.denied) return;
    }
    final position = await Geolocator.getCurrentPosition();
    setState(() {
      _pickupLocation = LatLng(position.latitude, position.longitude);
    });
  }

  Future<void> _requestRide() async {
    if (_pickupLocation == null || _dropoffLocation == null) return;
    setState(() => _isLoading = true);
    try {
      final ride = await ref.read(rideProvider.notifier).requestRide(
        riderId: 'current_user_id', // Get from auth state
        pickupLat: _pickupLocation!.latitude,
        pickupLng: _pickupLocation!.longitude,
        pickupAddress: _pickupAddress ?? '',
        dropoffLat: _dropoffLocation!.latitude,
        dropoffLng: _dropoffLocation!.longitude,
        dropoffAddress: _dropoffAddress ?? '',
        rideType: _selectedRideType,
        fareEstimate: _calculateEstimate(),
        paymentMethod: 'card',
      );
      if (mounted) {
        context.push('/ride/tracking/${ride.id}');
      }
    } catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(e.toString())),
      );
    } finally {
      if (mounted) setState(() => _isLoading = false);
    }
  }

  double _calculateEstimate() {
    // Simplified fare calculation
    return _selectedRideType == 'uberX' ? 12.50 : _selectedRideType == 'uberXL' ? 18.00 : 10.50;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Request a Ride')),
      body: Column(
        children: [
          Container(
            height: 200,
            decoration: BoxDecoration(
              color: Colors.grey[200],
              borderRadius: const BorderRadius.only(
                bottomLeft: Radius.circular(20),
                bottomRight: Radius.circular(20),
              ),
            ),
            child: const Center(child: Text('Map View - Pickup/Dropoff selection')),
          ),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                _LocationInput(
                  icon: Icons.my_location,
                  hint: 'Pickup location',
                  address: _pickupAddress,
                  onTap: () => _selectLocation('pickup'),
                ),
                const SizedBox(height: 12),
                _LocationInput(
                  icon: Icons.location_on,
                  hint: 'Destination',
                  address: _dropoffAddress,
                  onTap: () => _selectLocation('dropoff'),
                ),
                const SizedBox(height: 24),
                const Text('Select Ride Type', style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                const SizedBox(height: 12),
                ..._rideTypes.map((type) => _RideTypeCard(
                  type: type,
                  isSelected: _selectedRideType == type.id,
                  onTap: () => setState(() => _selectedRideType = type.id),
                )),
                const SizedBox(height: 16),
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: Colors.grey[100],
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      const Text('Fare estimate', style: TextStyle(fontSize: 16)),
                      Text(
                        '£${_calculateEstimate().toStringAsFixed(2)}',
                        style: const TextStyle(fontSize: 20, fontWeight: FontWeight.bold, color: Colors.green),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 20),
                SizedBox(
                  width: double.infinity,
                  child: ElevatedButton(
                    onPressed: _isLoading || _pickupLocation == null || _dropoffLocation == null
                        ? null
                        : _requestRide,
                    style: ElevatedButton.styleFrom(
                      backgroundColor: Colors.green,
                      padding: const EdgeInsets.symmetric(vertical: 16),
                    ),
                    child: _isLoading
                        ? const CircularProgressIndicator(color: Colors.white)
                        : const Text('Request Ride', style: TextStyle(fontSize: 16)),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  void _selectLocation(String type) {
    // In production: open place picker
  }
}

class _LocationInput extends StatelessWidget {
  final IconData icon;
  final String hint;
  final String? address;
  final VoidCallback onTap;
  const _LocationInput({required this.icon, required this.hint, this.address, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        decoration: BoxDecoration(
          border: Border.all(color: Colors.grey.shade300),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Row(
          children: [
            Icon(icon, color: Colors.green),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                address ?? hint,
                style: TextStyle(color: address != null ? Colors.black : Colors.grey),
              ),
            ),
            const Icon(Icons.search, color: Colors.grey),
          ],
        ),
      ),
    );
  }
}

class RideTypeCard extends StatelessWidget {
  final RideType type;
  final bool isSelected;
  final VoidCallback onTap;
  const RideTypeCard({required this.type, required this.isSelected, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      decoration: BoxDecoration(
        border: Border.all(color: isSelected ? Colors.green : Colors.grey.shade300, width: isSelected ? 2 : 1),
        borderRadius: BorderRadius.circular(12),
        color: isSelected ? Colors.green.withOpacity(0.05) : Colors.white,
      ),
      child: ListTile(
        leading: Icon(type.icon, color: isSelected ? Colors.green : Colors.grey),
        title: Text(type.name, style: TextStyle(fontWeight: FontWeight.bold)),
        subtitle: Text('${type.price} • ${type.capacity} • ${type.luggage}'),
        trailing: Radio<String>(
          value: type.id,
          groupValue: isSelected ? type.id : null,
          onChanged: (_) => onTap(),
          activeColor: Colors.green,
        ),
        onTap: onTap,
      ),
    );
  }
}

class RideType {
  final String id;
  final String name;
  final IconData icon;
  final String price;
  final String capacity;
  final String luggage;
  final bool isElectric;

  RideType({
    required this.id,
    required this.name,
    required this.icon,
    required this.price,
    required this.capacity,
    required this.luggage,
    this.isElectric = false,
  });
}