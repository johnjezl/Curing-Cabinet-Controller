# ADR 0205: Reading the DHT22

Status: accepted, not verified with a real sensor

## Options
1. Kernel `dht11` IIO driver through the `dht11` overlay. It also supports DHT22/AM2302 and
   timestamps edges in its interrupt handler. It claims the interrupt before releasing the
   line, so it cannot miss the sensor's response. Costs: it reports only ETIMEDOUT (too few
   edges) or EIO, so a broken line looks like an absent sensor, and a dead sensor takes about
   a second to fail.
2. User-space decode over the GPIO character device, using kernel edge timestamps. Gives finer
   errors (no edges, stuck low, wrong bit). But edge detection has to be armed by an ioctl after
   the line is released, and the sensor answers 20 to 40 us later, so the first edges can be lost.
3. Direct register bit-banging. Preemption breaks a 26 versus 70 us decode.

## Decision
Option 1 by default. Option 2 exists as `DHTGPIO`, with pulse-train tests, as the fallback if the
kernel driver misbehaves with the real sensor. It tolerates a lost preamble but has not run on
the rig, and it needs the overlay removed.

The driver reads on a background goroutine every 2.5 s, since the kernel driver's one-second
timeout is over the hal's 500 ms bound. `Read` returns the newest good sample with its true age,
for up to 10 s. After that it returns the last error. ETIMEDOUT maps to `ErrAbsent` and EIO to
`ErrBadRead`.

## Consequences
- 10 s of staleness plus the 2.5 s cycle is the slowest a lost sensor is reported, inside the
  roughly 30 s the benchmark documents for showing a lost probe.
- A wired but faulty sensor that yields few edges reports as absent. This costs diagnosis, not
  safety, if fault logic treats both the same way.
