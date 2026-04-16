-- name: top_rated_blocked_drivers
SELECT d.id, u.display_name, d.rating
FROM drivers d
         JOIN users u ON u.id = d.user_id
WHERE d.status = 'blocked'
ORDER BY d.rating DESC
    LIMIT 10;


-- name: latest_ride_status
SELECT DISTINCT ON (ride_id)
    ride_id, to_status, created_at
FROM ride_status_events
ORDER BY ride_id, created_at DESC, id DESC;


-- name: failed_payments_by_amount
SELECT p.id, p.amount, pm.user_id
FROM payments p
         JOIN payment_methods pm ON pm.id = p.payment_method_id
WHERE p.status = 'failed'
ORDER BY p.amount DESC
    LIMIT 100;


-- name: top_final_fare_quotes
SELECT rfq.ride_id, rfq.amount, r.status AS ride_status
FROM ride_fare_quotes rfq
         JOIN rides r ON r.id = rfq.ride_id
WHERE rfq.kind = 'final'
ORDER BY rfq.amount DESC
    LIMIT 10;


-- name: pickup_stops_with_coordinates
SELECT rs.ride_id, a.lat, a.lon
FROM ride_stops rs
         JOIN addresses a ON a.id = rs.address_id
WHERE rs.kind = 'pickup'
ORDER BY rs.ride_id
    LIMIT 100;