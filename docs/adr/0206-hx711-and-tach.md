# ADR 0206: HX711 clocking and tach measurement

Status: accepted, not verified with real devices

## HX711
The chip powers down if SCK stays high over 60 us, and user space can be preempted.

Options:
1. Bit-bang through the GPIO character device, measure each high time, discard a frame above
   50 us. Standard kernel interface, no privileges beyond group gpio.
2. Bit-bang through `/dev/gpiomem0` registers. Faster, but it bypasses the kernel's line
   ownership.
3. The kernel's `hx711` IIO driver. Would need a custom overlay and validating another driver.
4. RP1 PIO. Hard timing, more complexity.

Chosen: 1. Measured SCK high time on the rig is in `evidence/b/27-hx711-clock-high-time.txt`.
A slow pulse finishes the frame so the chip stays in step, and the read is retried inside the
190 ms budget. An unready chip gives `ErrAbsent`; DOUT stuck low gives `ErrBadRead` from the
25th-pulse check; a saturated reading gives `ErrBadRead`.

## Tach
Stopped and unplugged look identical on the wire.

Options:
1. Always 0 with no error. An unplugged fan would never be noticed.
2. Error whenever there are no pulses. A fan commanded to stop would fault.
3. Decide by what was commanded: `ErrAbsent` only after 5 s at 30 % duty or more with no pulses,
   else 0.

Chosen: 3. The 30 % threshold is an assumption about the P12 Pro, not a measurement. If the real
fan needs a different threshold the constant is in `fan.go`.

Edges come with kernel timestamps. Those under 3 ms apart are dropped, and more than 4000 RPM
is a bad read, as PWM crosstalk is the likely cause.
