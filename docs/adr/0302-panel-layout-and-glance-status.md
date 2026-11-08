# ADR 0302: Layout and the glance status

Status: accepted

## Context
The panel is 108 x 65 mm, 7.4 px per mm. It is read across a room. The reference
digit height is 14 to 30 mm, or 104 to 222 px, and four live values must fit plus
status and navigation.

## Options
1. Four equal tiles. Fair to all values, but no value reaches 14 mm.
2. A hierarchy: temperature and humidity at 14 mm or more, airflow and weight about
   half that, with a status banner above.
3. Paged values with one huge digit at a time. A glance may land on the wrong page.

## Decision
Option 2. Temperature and humidity digits are 118 px (16 mm) in the normal layout
and 104 px (14 mm) when a large banner is showing. A test enforces 104 px or more
on Home for every fixture with a live reading.

A single status, computed by one pure function, drives the banner on every screen.
Its order is: controller not responding, active alarm by severity, unusable reading,
door open, logging or watchdog fault, recipe ended, out of band with no alarm yet,
paused, defrost, then OK. When there is something to do, Home's banner grows and
states it. A green banner says "OK: NO ALARMS, IN RANGE": it describes the cabinet,
not the product.

Trends are small multiples with one measure per axis, not a dual axis. A gap in
valid data is hatched and never bridged by a line.

## Consequences
Airflow and weight are 6 mm digits, not readable from as far away. Space for holds
comes from those tiles: in the tightest state (alarm plus hold) they are 4 mm.

## Amendment after the integration critic (defects 2 and 7)
A reading's deviation colour started at the alarm band (2.5 °C, 10 %RH), so humidity 8 or 9 % over setpoint showed
white under "OK: NO ALARMS, IN RANGE". Colour now starts at the regulation band, about ±1 °C and ±5 %RH (or the alarm
band if that is narrower), and "IN RANGE" is shown only when both readings are inside it; otherwise the green line reads
"OK: NO ALARMS". The amber "no alarm yet" headline still waits for the alarm band, so a compressor run's normal humidity
swing colours the digits but does not take the headline. The output reasons state the error of the number on the panel,
not of the slower average the loop acts on, and say what the output is doing about it (ADR 0101).
