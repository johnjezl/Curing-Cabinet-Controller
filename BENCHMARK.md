# BENCHMARK: what commercial dry-aging cabinets and refrigeration controllers actually deliver

Research date: 2026-11-06. Frozen record; written before any controller existed. Nothing here was shaped around a design.

## 0. How to read this document

**Number labels.** Every number carries one of:

- **DOCUMENTED**: a manufacturer (or, for section 7, a regulator or standards body) states it. A documented number is a claim, not a measurement.
- **REPORTED**: a third-party measurement or review.
- **INFERRED**: my reasoning, stated beside it.

"not published" means I looked and did not find it. Marketing with no tolerance is marked **[MARKETING, no tolerance]**.

**Source markers.** `S#` links go to the URL; the full list is in section 8. `S#*` means I saw only a search-result excerpt because the page refused my fetch. Treat those as weaker.

**The biggest finding: there is almost no measurement.** No cabinet manufacturer I could read publishes a steady-state stability band, a door-recovery time, or a test method. I found no independent logged test of a premium cabinet. The strongest evidence is controller parameter tables, which state what a controller can be configured to do. They do not state what a built cabinet achieves. Many of the "reference" thresholds below are therefore controller defaults or inferences from them, and they are labelled so. A stronger-sounding bar would be fiction.

**Composite warning.** No single product does everything below. Each Reference behavior block is a composite and says when a statement comes from one product only or from a default that the cabinet builder can change. Controller defaults are routinely overridden by cabinet builders.

**Age of the documents.** Several controller manuals are old: XR06CX r1.2 (2007), XLH360 r1.0 (2005), ir33 rel. 2.5 (2010). These product families are still widely sold, but newer firmware may differ.

**Fetch limits.** manualmachine, manualshive, gev-online, PMC, AmazingRibs, Smoking Meat Forums and Zanduco refused automated fetches, and the Emerson host did not resolve. FSIS documents were refused at origin and read through the Internet Archive copy. Consequences: the Dixell XW series is only partly covered, and owner reports are excerpts.

### Products covered

| Product | Type | What I actually read |
|---|---|---|
| DRY AGER DX 1000 Premium S | Cabinet | Full English manual, 2021 [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) |
| DRY AGER DX 1000 (earlier) | Cabinet | German manual, 2017 [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf) |
| DRY AGER UX 1500 Pro S / UX 750 Pro | Cabinet | US manual [S3](https://www.webstaurantstore.com/documents/pdf/dryager_use_and_care_manual_ux1500_pro_s.pdf), spec sheet [S4](https://www.webstaurantstore.com/documents/specsheets/td_ux0750p_2009_1.pdf) |
| SteakAger PRO 20/40 | Cabinet | Full user guide [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| Klarstein Steakhouse Pro | Cabinet | Multi-language manual [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf) |
| Stagionello / Maturmeat (Omcan) | Cabinet, closest to a curing product | Spec sheets [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf) [S13](https://argo.webstaurantstore.com/documents/specsheets/curing_specs.pdf) |
| Dixell XR06CX | Controller | Full manual r1.2 [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) |
| Dixell XLH360 | Controller for seasoning and maturing cabinets, closest to this use | Full manual r1.0 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) |
| Dixell XW60K | Controller | Partial [S18](https://www.manualslib.com/manual/1665451/Dixell-Xw60k.html), plus a cabinet builder's manual using it [S17](https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf) |
| Carel ir33 platform | Controller | Full manual rel. 2.5 [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1), leaflet [S21](https://ish.carel.com/documents/10191/0/%2B050003061/672869f3-306b-46d6-881d-e9f22156cc65?version=1.0) |
| Eliwell IDW961/971/974 | Controller, same parameter names as ID 974 [S25*](https://manualmachine.com/eliwell/id974/21475188-user-manual) | Full manual, Spanish [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf) |

---

## 1. Control stability

### 1.1 Ranges a cabinet offers

| Product | Temperature | Humidity | Tolerance stated | Label |
|---|---|---|---|---|
| DX 1000 Premium S | 0 to +30 °C | 40 to 90 %RH | None. "precise temperature control" **[MARKETING, no tolerance]** | DOCUMENTED [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) |
| DX 1000 Premium S factory default | 1.5 °C | 82 %RH | None | DOCUMENTED [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) |
| UX 750 Pro | 0 to 30 °C (32 to 86 °F) | 40 to 90 %. "steady humidity" **[MARKETING, no tolerance]** | None | DOCUMENTED [S4](https://www.webstaurantstore.com/documents/specsheets/td_ux0750p_2009_1.pdf) |
| UX 1500 Pro S | 0 to 30 °C (spec sheet) | Spec sheet 40 to 90 % [S39](https://www.webstaurantstore.com/documents/specsheets/td_ux1500p_2102_1_1_.pdf); manual 60 to 90 % [S3](https://www.webstaurantstore.com/documents/pdf/dryager_use_and_care_manual_ux1500_pro_s.pdf), an internal conflict | None | DOCUMENTED |
| DX 500 (reseller page) | 0 to +25 °C "with 0.1 °C precision" | 60 to 90 % "with 0.5 % precision" | Not a stability figure; INFERRED to be display or setting resolution, because the same page states no band or test | DOCUMENTED by reseller only [S5](https://www.metos.fi/en/p/meat-maturing-fridge-metos-dry-ager-dx-500-p_mg4620018) |
| SteakAger PRO 40 | User-set; see 1.2 | Fixed 75 % average; humidifier optional | See 1.2 | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| Klarstein Steakhouse Pro | 1 to 25 °C | 60 to 85 % | ±2 °C, see 1.2 | DOCUMENTED [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf) |
| Stagionello twin (retailer) | 0 to +35 °C | 30 to 99 %RH | not published | Retailer [S14*](https://www.zanduco.com/us/omcan-stagionello-twin-100-100-kg.html) |

The humidity range varies across one brand's documents (40 to 90 versus 60 to 90), so a range quoted for "a DRY AGER" needs a model and a document.

Trap in the Stagionello data: retailers quote "-5 to 50 °C working range". The Omcan sheet lists that under the controller's ambient conditions, so it is not a cabinet range [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf). On the same sheet, the "resolution" and "probe reading precision (temperature)" cells repeat values from other rows (-10 to 70 °C, "Less than 90 % RH"), and humidity precision says "See Humidity Probe Characteristics". INFERRED: a layout error. **Stagionello probe precision is effectively not published.**

### 1.2 Stated bands

These are the only cabinet-level band statements I found. None has a test method.

| Source | Statement | Label |
|---|---|---|
| SteakAger, dry aging | Set 40 °F. Set temperature is the upper limit; cycles "from 33F to 40F", averaging 37 °F. That is a 7 °F (3.9 °C) span | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| SteakAger, charcuterie | Set 59 °F to average 55 °F | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| SteakAger | "You will observe temperature and humidity cycling widely between each compressor cycle... We only care about average temp and humidity over time." | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| SteakAger humidifier | Off for the first hour; then acts on the average against 75 %, changing state after about 1 h. A second page says only below 75 % for 3 h. The two pages differ | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| Klarstein | Thermostat holds "within a range of +/- 2 °C"; through thermal inertia the actual temperature "will only vary by 0.5 ºC to 1 ºC". The second figure has no method: **[MARKETING, no test method]** | DOCUMENTED [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf) |
| Klarstein | Wait about 12 h to see the effect of a setting when fully loaded | DOCUMENTED [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf) |
| Cabinet builder using XW60K (a commercial fridge, not a dry ager) | "The product will work with + 3 C° difference" | DOCUMENTED [S17](https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf) |
| DRY AGER, Stagionello | not published | n/a |

### 1.3 Controller regulation

| Controller | Temperature method | Default band | Humidity method | Default band | Label |
|---|---|---|---|---|---|
| XR06CX | On/off, cuts in at set + Hy, out at set | Hy 2.0 °C (range 0.1 to 25) | none | n/a | DOCUMENTED [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) |
| XLH360 | Neutral zone; cooling cuts in at set + dbt, heater at set - dbt, both out at set | dbt 2.0 °C (0.1 to 25) | Neutral zone, humidifier and dehumidifier | dbH 5.0 %RH (0.5 to 50) | DOCUMENTED [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) |
| Eliwell ID family | On/off with differential | diF 2.0 (0.1 to 30) | none | n/a | DOCUMENTED [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf) |
| Carel ir33 | On/off with delta | rd 2.0 °C (0.1 to 20) | none | n/a | DOCUMENTED [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) |

**INFERRED band.** A default commercial setup cycles from setpoint to setpoint + 2.0 °C at the probe, and humidity moves ±5 %RH around setpoint before action. Real air swings exceed this through overshoot and coil effects; how much is not published. A shop can tighten these parameters, down to 0.1 °C and 0.5 %RH, but the penalty is short-cycling, which the protection timers in section 3 then limit.

On XLH360, when cooling and dehumidifying are both requested, cooling has priority and the heater joins once set temperature is reached. Dehumidification without a dedicated relay runs compressor and heater together [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) (DOCUMENTED, no number).

### 1.4 Measurement

| Item | Value | Label |
|---|---|---|
| XR06CX | NTC -40 to 110 °C; resolution 0.1 °C or 1 °C or 1 °F; accuracy ±0.1 °C ±1 digit at 25 °C ambient | DOCUMENTED [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) |
| XLH360 | NTC -40 to 110 °C; resolution 0.1 °C; accuracy ±0.5 °C ±1 digit at 25 °C ambient; humidity from a 4-20 mA transmitter, with humidity accuracy not stated | DOCUMENTED [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) |
| Eliwell | Better than 0.5 % of full scale +1 digit; resolution 0.1 °C. It states the figures refer to the instrument only, not to probes | DOCUMENTED [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf) |
| Carel, standard NTC | Measurement error 1 °C over -50 to 50 °C; 3 °C up to 90 °C | DOCUMENTED [S21](https://ish.carel.com/documents/10191/0/%2B050003061/672869f3-306b-46d6-881d-e9f22156cc65?version=1.0) |
| Carel | Display tenths between -19.9 and +19.9 °C only | DOCUMENTED [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) |
| Carel | Claims EN 13485 (air, S, A, 1, -30 to +30 °C) with its NTC probe. Standard text not retrieved, so the tolerance behind "class 1" is unverified here | DOCUMENTED [S22](https://ish.carel.com/ir33-series/-/journal_content/56_INSTANCE_i4q5KIMLInKK/10191/25234) |
| Probe calibration offset | XR06CX ±9.9 °C; XLH360 ±12.0 °C and ±10 %RH | DOCUMENTED [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) |
| DRY AGER, SteakAger, Klarstein probe accuracy | not published. DRY AGER's troubleshooting tells the user to check with an independently inserted thermometer [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). SteakAger says aftermarket sensors "will never read exactly the same" [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) | n/a |
| Stagionello | 4 probes (humidity, ambient, evaporator, condenser); humidity 0-99 %RH, 4-20 mA; precision not published | DOCUMENTED [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf) |

Note the gap between ±0.1 °C (instrument electronics, XR06CX) and 1 °C (a probe-inclusive figure, Carel). A ±0.1 °C controller figure is not system accuracy.

### 1.5 Airflow

- Cabinets: DRY AGER "Optimum air flow" with activated carbon and UVC, no velocity given **[MARKETING, no tolerance]** [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). Stagionello "automatically manages... air speed", no figure [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf). **Air velocity is not published by any cabinet I read, and no manual describes measuring it.**
- Controllers: XLH360 offers fan modes (with load or continuous; on or off in defrost) and timed air-exchange cycles, interval up to 120 h and duration up to 250 min, both defaulting to 0, meaning manual start and stop [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) (DOCUMENTED).
- Measured in rooms: ten commercial US dry-aging facilities measured 0.74 to 5.26 °C, 64.87 to 99.21 %RH, 0.56 to 2.03 m/s [S28*](https://academica-e.unavarra.es/handle/2454/42679) (REPORTED; beef). A study's stated chamber conditions: 1 ± 1 °C, 80 ± 5 %RH, 0.5 to 2 m/s [S29](https://mdpi-res.com/d_attachment/foods/foods-11-01526/article_deploy/foods-11-01526.pdf) (REPORTED; beef).

That spread matters: rooms in the trade span about 34 RH points and 4.5 °C. The study reports differences in flavor and acceptability; pH and water activity did not differ. It is not a safety study, and it is not evidence of tight control in the trade.

### 1.6 Reported performance and shortfalls

- One owner blog says during run-in "The temperature and humidity held steady", with no data, at 82 % and 34 °F [S8](https://www.nicksolares.com/post/adventures-in-dry-aging-the-arrival) (REPORTED, unmeasured).
- UX 1000 forum thread: an owner questions whether real conditions match settings, and another recommends an independent monitor and says of the issue "seems everyone with one has this" [S9*](https://www.smokingmeatforums.com/threads/ux-1000-dry-ager.328606/) (REPORTED, anecdotal; numbers not retrieved).
- A low-end peer (CASO 125): owner reports true readings of 9 °C and 96 % against other displayed values [S10*](https://www.smokingmeatforums.com/threads/caso-125-dry-ager-issues.323896) (REPORTED; displayed values not retrieved).
- DRY AGER: "The humidity control only works properly when the unit is loaded with food" [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED, no number). This is a real limit for empty-cabinet testing.

### Reference behavior: control stability

- R1.1 The system accepts temperature setpoints over at least 0 to 30 °C and humidity setpoints over at least 40 to 90 %RH. (DOCUMENTED S1, one product at this width.)
- R1.2 At an aging setpoint, the temperature at the control probe cycles from the setpoint to about 2.0 °C above it. (INFERRED from default on/off differentials in S15, S16, S20, S24, ignoring overshoot.)
- R1.3 When heating is also available, temperature below the setpoint by more than about 2.0 °C triggers heating. (DOCUMENTED default, S16.)
- R1.4 Humidity at the probe is corrected once it is more than about 5 %RH from setpoint, and correction stops at setpoint. (DOCUMENTED default, S16; achieved band not published.)
- R1.5 Cabinet-level swings of about ±2 °C are stated as normal by one product (DOCUMENTED S11); a 3.9 °C span with a mean about 1.7 °C under the set value by another (DOCUMENTED S6, span and mean computed from °F).
- R1.6 One product states only a long-term 75 %RH average and calls wide humidity cycling between cooling cycles normal. (DOCUMENTED S6.) No system publishes a logged steady-state humidity band.
- R1.7 Temperature is displayed to 0.1 °C. (DOCUMENTED S15, S16, S20.) Probe-inclusive measurement error is about 1 °C. (DOCUMENTED S21.) Instrument-only accuracy is ±0.1 to ±0.5 °C ±1 digit at 25 °C. (DOCUMENTED S15, S16.)
- R1.8 The operator can apply a calibration offset to the temperature reading (up to ±9.9 or ±12 °C) and the humidity reading (±10 %RH). (DOCUMENTED S15, S16.)
- R1.9 Air circulates in the chamber. Measured velocity in commercial rooms is 0.56 to 2.03 m/s. (REPORTED S28.) Air velocity in cabinets: not published.

---

## 2. Recovery after disturbances

**No manufacturer I could read publishes a door-recovery time, a warm-load recovery curve, or a humidity-recovery time. I found no independent measurement either.** This is the weakest area. Everything numeric below is a proxy.

### 2.1 Alarm-suppression windows as a proxy

Controllers ship with windows that suppress alarms after a disturbance. INFERRED reasoning: a manufacturer sets these so a healthy system normally recovers inside them without a nuisance alarm, so they suggest an upper bound on expected recovery. They are not measurements, and builders change them.

| Disturbance | Window (default) | Source | Label |
|---|---|---|---|
| Power-on, temperature alarm | 90 min (0 to 99) | XR06CX [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) | DOCUMENTED |
| Power-on, temperature and humidity alarm | "1.3" on a 0.0 to 23.5 h scale; INFERRED to mean 1 h 30 min, because the parameter text says 0 min to 23 h 50 min | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | DOCUMENTED value, INFERRED reading |
| After door closes, temperature | 20 min | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | DOCUMENTED |
| After door closes, humidity | 20 min (doH default; its text describes end of defrost, while section 10.1 assigns dot and doH to door closing, so the manual is ambiguous) | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | DOCUMENTED |
| After defrost, temperature | 20 min | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | DOCUMENTED |
| After defrost | 1 h | Carel d8 [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) | DOCUMENTED |
| Any excursion, delay before temperature alarm | 15 min XR06CX, 15 min XLH360, 120 min Carel | [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) | DOCUMENTED |
| Any excursion, delay before humidity alarm | 15 min | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | DOCUMENTED |

### 2.2 Door opening

- During door open, controllers commonly stop cooling or fans. XR06CX default: compressor and fans off, regulation resuming after the door-alarm delay (default 5 min) [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf). XLH360 default: fans off, outputs restart with the door alarm (default 20 min) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Carel: cooling and fans off, temperature alarms disabled; after d8 (default 1 h) normal running resumes with buzzer and alarm relay [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). All DOCUMENTED.
- The DRY AGER alarm table says regulation is unaffected by the door alarm [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED). Its interior light comes on with the door; door behaviour of fans and cooling is not published.
- Guidance is "Open the unit for as short as time as possible" [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf), with no recovery figure.

### 2.3 Warm product

- Manufacturers largely instruct users to avoid this. DRY AGER: cold chain must be maintained [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). SteakAger: observe normal operation "for a few hours before loading" [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf). The XW60K cabinet builder: reach set temperature before loading, or the cabinet temperature "will not drop", noting fans stop above a certain temperature [S17](https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf). All DOCUMENTED with no recovery time beyond the "few hours".
- Empty-cabinet pull-down: "When the cabin is empty the setting temperature must be reached in 60 minutes" [S17](https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf) (DOCUMENTED, commercial fridge; setpoint and ambient not stated).
- Fully loaded settling: about 12 h to see a setting's effect [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf) (DOCUMENTED).
- Ambient limits bound any of this. DX 1000 Premium S: 10 to 35 °C at up to 75 % RH [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). UX: 50 to 95 °F, with 86 °F maximum to meet NSF [S3](https://www.webstaurantstore.com/documents/pdf/dryager_use_and_care_manual_ux1500_pro_s.pdf). SteakAger: above 50 and below 90 °F [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf). Stagionello: up to +32 °C [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf). All DOCUMENTED.

### 2.4 Defrost

- Automatic defrost is standard on DRY AGER [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf); a manual start existed on the earlier model [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf).
- During defrost, XLH360 disables temperature control, and humidity control is off by default [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) (DOCUMENTED). So both humidity and temperature are expected to move.
- Defaults: XR06CX every 6 h, maximum 30 min [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf). XLH360 maximum 20 min [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Eliwell every 6 h, maximum 30 min [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf). Carel every 8 h [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). All DOCUMENTED.
- Magnitude and recovery time of the defrost disturbance: not published.

### 2.5 Power loss

- Klarstein: "A power cut of one or two hours will not affect the temperatures" [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf). **[MARKETING, no test method]**. Physically this can only mean limited drift.
- DRY AGER: power should not be interrupted "for long periods of time"; no figure [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED, no number).
- Recovery time after power returns: not published. Restart behaviour is in section 4.

### Reference behavior: recovery

No measured recovery time is available for any product. The bounds here are proxies.

- R2.1 After the door closes, temperature and humidity alarms stay suppressed for about 20 min. (DOCUMENTED default S16.) A healthy system is expected to be back inside its alarm band by then. (INFERRED from that window.)
- R2.2 When the door opens, cooling and/or air circulation stop, then resume after a set delay if the door stays open: 5 to 20 min on two products, 60 min on a third. (DOCUMENTED defaults S15, S16, S20.) One product reports no regulation change for a door alarm. (DOCUMENTED S1.)
- R2.3 After a defrost, temperature alarms stay suppressed for 20 to 60 min. (DOCUMENTED defaults S16, S20.) A healthy system is expected to recover by then. (INFERRED.)
- R2.4 From power-on, temperature alarms stay suppressed for about 90 min. (DOCUMENTED default S15, S16 as read.) A healthy system is expected to reach its band by then at refrigeration setpoints with no heavy load. (INFERRED.)
- R2.5 An empty refrigerated cabinet reaches setpoint within 60 min. (DOCUMENTED S17, general refrigerator, conditions not stated.)
- R2.6 A fully loaded cabinet may take about 12 h to show a setting change. (DOCUMENTED S11, one product.)
- R2.7 Warm product is not a supported operating case: the instruction is to load cold product into a cabinet already at setpoint. (DOCUMENTED S1, S6, S17.) Warm-load recovery: not published.
- R2.8 Humidity recovery after any disturbance: not published. Humidity control is off during defrost by default in the one humidity controller read. (DOCUMENTED S16.)
- R2.9 One humidity system withholds humidification for the first hour after start. (DOCUMENTED S6.)

---

## 3. Equipment protection

### 3.1 Parameters

Range and default for each controller. All DOCUMENTED. A default of 0 means the protection is available but off until the builder sets it.

| Function | XR06CX [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | Carel ir33 [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) | Eliwell ID [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf) |
|---|---|---|---|---|
| Minimum off (stop to restart) | AC 0 to 50 min, default 1 | AC 0 to 30 min, default 1 | c2 0 to 15 min, default 0 | dOF 0 to 250 min, default 0 |
| Minimum between starts | not separate | not separate | c1 0 to 15 min, default 0 | dbi 0 to 250 min, default 0 |
| Minimum on | not in parameter list | not in parameter list | c3 0 to 15 min, default 0 | not in parameter list |
| Delay after request | no | no | no | dOn 0 to 250 s, default 0 |
| Restart delay at power-up | od 0 to 99 min, default 0 | OdS 0 to 255 min | c0 0 to 15 min, default 0 | OdO 0 to 250 min, default 0 |
| Fault duty-cycle fallback | Cy 0 to 99 min on, default 15; Cn 0 to 99 min off, default 30 | none: outputs off | c4 0 to 100 min on, default 0 (off); fixed 15 min off; 100 = always on | Ont 0 to 250, default 0; OFt 0 to 250, default 1, which together mean off |
| Defrost at power-on | dP, default no | dPO selectable | d4, default off | dPO, default no |
| Fan stop by evaporator temperature | FS default 2.0 °C | yes | F1 default 5.0 °C | FSt default 50 °C |
| Drip time after defrost | dt default 0 | Fdt default 0 | dd selectable | dt default 0 |
| Fan delay after defrost | Fd default 10 min | selectable | Fd default 1 min | Fdt default 0 |

XW60K and XW270K: AC 0 to 30 min, default 4 [S26*](https://www.intarcon.com/pdfs/EN/XW270K.pdf) (DOCUMENTED, excerpt only). Examples of how Carel explains this: c1 = 6 min limits to at most 10 starts per hour; c0 = 6 means a six-minute wait after power returns [S23](https://www.manualslib.com/manual/1649829/Carel-Ir32m.html?page=20) (DOCUMENTED).

### 3.2 Notable points

- **Indication.** XR06CX, XLH360 flash the compressor LED during the anti-short-cycle delay [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). DRY AGER flashes its cooling LED for "Switch-on delay cooling" [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf), duration not published.
- **Defrost overriding protection.** Carel d9 = 1 lets defrost ignore compressor times; d9 = 0 respects them [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). Carel with c4 = 0 shuts the compressor immediately on a probe fault "without observing the compressor protection times" [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). Both DOCUMENTED.
- **Condenser protection.** Carel, if a condenser probe is fitted: pre-alarm above Ac + AE/2, cleared below Ac; alarm above Ac + AE stops the compressor, is manual-reset, and is also raised on a condenser probe fault. Default Ac 70 °C, AE 5 °C (the leaflet shows 10), delay 0 min [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) [S21](https://ish.carel.com/documents/10191/0/%2B050003061/672869f3-306b-46d6-881d-e9f22156cc65?version=1.0) (DOCUMENTED). XW60K lists HA2/LA2 alarms and a condenser temperature alarm delay [S18](https://www.manualslib.com/manual/1665451/Dixell-Xw60k.html) (DOCUMENTED, partial, part from a third-party FAQ on that page). Stagionello lists a condenser probe [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf) (DOCUMENTED). XR06CX and XLH360 have no condenser probe [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). DRY AGER, SteakAger and Klarstein: not published; DRY AGER asks for annual dust cleaning of the heat exchanger [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf).
- **Pressure switch.** XLH360 can stop on a count of pressure-switch events (up to 15 in a window) and then requires power-cycling to clear [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) (DOCUMENTED).
- **Filtered sensing.** Carel /2 filters measurement, default 4 of 1 to 15 [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) (DOCUMENTED).
- **Compressor settling at install.** SteakAger: wait 24 h before first power-on for compressor oil [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) (DOCUMENTED).
- **Limit.** Dixell states the XR06CX "cannot be used as a safety device" [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) (DOCUMENTED).

### Reference behavior: equipment protection

- R3.1 After cooling stops, it does not restart until a minimum off time has passed: settable up to 15 to 250 min, shipped at 1 to 4 min on some products and at 0 (off) on others. (DOCUMENTED S15, S16, S20, S24; 4 min from S26.)
- R3.2 Cooling starts can also be spaced by a minimum interval, and a minimum on-time can be set, up to 15 min. (DOCUMENTED S20; interval also S24.)
- R3.3 After mains power returns, outputs can be held off for a settable delay of up to 15 to 255 min, default 0. (DOCUMENTED S15, S16, S20, S24.) One product shows a flashing indicator during this wait. (DOCUMENTED S1.)
- R3.4 While a protection timer holds off cooling, the front panel shows this by a flashing indicator. (DOCUMENTED S1, S15, S16.)
- R3.5 On loss of the control temperature reading, one product family's default runs cooling 15 min on and 30 min off. (DOCUMENTED S15.) Others default to cooling off, with on-time settable. (DOCUMENTED S16, S20, S24.) One uses a settable on-time with a fixed 15 min off. (DOCUMENTED S20.)
- R3.6 Defrost starts on a schedule of about 6 to 8 h by default (DOCUMENTED S15, S20, S24) and is capped at 20 to 30 min (DOCUMENTED S15, S16, S24). Defrost ends on evaporator temperature, or on time if that reading fails. (DOCUMENTED S1, S15, S16, S24.)
- R3.7 Air circulation can be held off briefly after defrost, default 0 to 10 min. (DOCUMENTED S15, S20, S24.)
- R3.8 Where a condenser temperature sensor is fitted, a pre-alarm precedes a shutdown, and the shutdown alarm requires a manual reset. Default threshold 70 °C plus a differential of 5 °C. (DOCUMENTED S20.) Most products read do not have this.

---

## 4. Failure behavior

### 4.1 Sensor faults

| Fault | DRY AGER DX 1000 Premium S [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | XR06CX [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) | Carel [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) | Eliwell [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf) |
|---|---|---|---|---|---|
| Control temperature | Cooling and heating OFF | Compressor and heating off | Duty cycle, 15 on / 30 off default | c4 duty; default off | Ont/OFt; default off |
| Evaporator | Defrost on time | Defrost on time | Defrost end timed | Signalled; no output change | Defrost ends on timeout |
| Humidity | Humidity control OFF | Humidity regulation off | n/a | n/a | n/a |
| Cleared | Automatic, about 30 s after repair on earlier model [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf) | 10 s after probe recovers | "some seconds" | Automatic | not stated |

All DOCUMENTED. Eliwell defines faults as short, open or out-of-range readings [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf).

**Three honest caveats:**

1. **Cooling off is not obviously fail-safe.** INFERRED: on loss of the control probe a cabinet with no cooling and no heating drifts to room temperature, so food is at risk unless someone responds to the buzzer. Conversely, a duty fallback at fixed on/off times can overcool or undercool, because it runs blind to load and setpoint. Both are design compromises; manuals do not claim otherwise. A fixed fallback sized for refrigeration may not transfer to curing temperatures (section 6).
2. **A stuck in-range sensor is not addressed.** No manual I read describes detecting a plausible but wrong reading, such as a drifted humidity probe. Fault logic documented is short/open/out-of-range. INFERRED absence, not a positive claim.
3. **No manual describes a secondary hardware cut-out.** Dixell's "cannot be used as a safety device" [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) and the DRY AGER disclaimer that the maker assumes no liability for damage to refrigerated goods "even if the unit is defective within the warranty period" [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) set expectations.

### 4.2 Power loss and resume

- Setpoints and parameters persist. Controllers: EEPROM [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). Klarstein: "memory function" reverts to the prior setpoint [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf), though another section says it "will revert to a preset temperature setting" [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf), so the manual contradicts itself.
- Program progress: "In the event of a power failure that occurs during an aging program, the program progress of the selected program remains saved" [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED). Whether the timer counts outage time: not published.
- Manual program change: changing program discards the previous one irrecoverably and starts a new remaining time [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED).
- Clock: XLH360 internal clock back-up 24 h [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Carel RTC error ±10 ppm at 25 °C [S21](https://ish.carel.com/documents/10191/0/%2B050003061/672869f3-306b-46d6-881d-e9f22156cc65?version=1.0) (DOCUMENTED).
- Recording the outage: Carel raises an HF alarm after a failure longer than 1 min if temperature on return exceeds the high threshold, storing time, duration and maximum temperature [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) (DOCUMENTED). XW60K-based cabinets: "it will detect a power outage and then when you rerun it will give an alarm" [S17](https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf) (DOCUMENTED). DRY AGER, SteakAger, Klarstein: outage record not published.
- Undervoltage: Carel's platform text says below a threshold the display switches off while the unit continues with relays energised [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) (DOCUMENTED, no voltage given).
- Memory corruption: XLH360 and XW60K flash EE, cleared by a key press showing "rSt" for about 3 s [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) [S18](https://www.manualslib.com/manual/1665451/Dixell-Xw60k.html). Carel has EE and EF, with EF allowing parameter re-save [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) (DOCUMENTED).

### 4.3 What stops outright

- Serious external alarm: XR06CX and XLH360 turn all outputs off, recovering when the input clears [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Pressure switch: XLH360 stops until power-cycled [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Eliwell external alarm blocks regulation if configured [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf).
- DRY AGER temperature and humidity limit alarms leave regulation unaffected [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). All these controllers treat a high/low alarm as information, not a shutdown [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1).

### Reference behavior: failure

- R4.1 Loss of the control temperature reading is shown on the display within about 30 s (DOCUMENTED S2, S15) and sounds an alarm. It either stops cooling and heating or runs a timed cooling cycle (settable, 15 min on / 30 min off on one default). (DOCUMENTED S1, S15, S16, S20, S24.)
- R4.2 Loss of the humidity reading stops humidity control while temperature control continues. (DOCUMENTED S1, S16.)
- R4.3 Loss of the evaporator temperature reading makes defrost run on time rather than temperature; temperature control continues. (DOCUMENTED S1, S15, S16, S24.)
- R4.4 A sensor fault clears itself about 10 to 30 s after the sensor recovers. (DOCUMENTED S2, S16.)
- R4.5 Setpoints and configuration survive power loss with no user action. (DOCUMENTED S15, S16, S20.) Program progress survives power loss. (DOCUMENTED S1.)
- R4.6 A clock survives at least a 24 h outage where a clock is present. (DOCUMENTED S16.)
- R4.7 An outage of more than 1 min, followed by temperature above the high limit on return, creates a stored alarm with time, duration and peak temperature. (DOCUMENTED S20; clock-equipped models only.)
- R4.8 High or low temperature and humidity alarms do not change regulation. (DOCUMENTED S1, S15, S16, S20.)
- R4.9 Nonvolatile memory errors are shown on the display and can be cleared by the operator. (DOCUMENTED S16, S18, S20.)
- R4.10 Detection of a plausible but wrong sensor reading: not described in any source.
- R4.11 The controller is not rated as a safety device. (DOCUMENTED S15.)

---

## 5. Alarming

### 5.1 Limits and delays

All DOCUMENTED.

| Item | XR06CX [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) | XLH360 [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) | Carel [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) | DRY AGER [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) |
|---|---|---|---|---|
| High/low temperature | AU/AL absolute; defaults 99 °C / -55 °C | ALU/ALL absolute or relative; defaults 110 / -40 | AH/AL relative or absolute; 0 = disabled | HA/LA; thresholds not published |
| High/low humidity | n/a | AHu/AHL; defaults 100 / 0 | n/a | HHA/LHA; thresholds not published |
| Recovery differential | not offered | 1.0 °C; 2.0 %RH | A0 2.0 °C | not published |
| Alarm delay | 15 min | 15 min; humidity 15 min | 120 min | not published |
| Door alarm delay | di 5 min | doA 20 min | d8 | "dA", delay not published |

**Point of honesty:** out-of-box limits on several controllers are effectively disabled (99 °C, 110 °C, or 0). The builder must set real values. Real cabinet alarm thresholds are not published by DRY AGER, SteakAger, Klarstein or Stagionello.

### 5.2 Signalling and acknowledgement

- DRY AGER: display message plus "audible alarm that can be acknowledged and turned off by pressing any button" [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). Messages disappear when the condition ends. Earlier model: audible alarm listed as optional, and stored min/max values viewable while locked [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf).
- XLH360: buzzer silenced with any key; `tbA` chooses whether the relay stays on after key press, default off; alarm display until recovery, alternating with room temperature except for P1 which flashes [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf).
- Eliwell: any key silences; icon keeps flashing [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf).
- Carel: PRG mutes buzzer and resets the alarm relay [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1).
- Most temperature alarms self-clear. XLH360 and DX 1000 (earlier) also clear HA/LA "when the defrost starts" [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). INFERRED weakness: a transient excursion leaves no trace on a unit without a log.
- Relays: XLH360 can assign an alarm relay [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Carel has a multifunction output for remote alarm [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). All DOCUMENTED.
- Door switch alarm clears when the door closes [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) (DOCUMENTED).
- Maintenance alarm: DRY AGER "UCC" for UVC lamp life and annual filter change [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED).

### 5.3 History and logging

| Product | Stored history | Label |
|---|---|---|
| Carel (RTC models) | Last 3 HA and 3 HF events, with counters to 15, time, duration and max temperature, in E2PROM, flagged by a HACCP LED. Separately, a min/max monitor runs up to 999 h | DOCUMENTED [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) |
| XW60K cabinet | 10 alarm records (HA or LA) with start and end date and time; all deleted with a 5 s key press | DOCUMENTED [S17](https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf) |
| Dixell XJDL logger | Up to 4 temperatures every 15 min, up to 1 year | DOCUMENTED [S19*](https://webapps2.copeland.com/ci/dixell-xweb/2018ECT-59%20Dixell%20Cold%20Chain%20Solutions.pdf) |
| Dixell XWEB | Internet monitoring for HACCP | DOCUMENTED [S19*](https://webapps2.copeland.com/ci/dixell-xweb/2018ECT-59%20Dixell%20Cold%20Chain%20Solutions.pdf) |
| XLH360 | Modbus RTU, RS485 optional, no internal log described | DOCUMENTED [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) |
| Stagionello | "evolved system of HACCP that meets all international standards" **[MARKETING, no tolerance]**; review from the touch screen | DOCUMENTED [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf) [S13](https://argo.webstaurantstore.com/documents/specsheets/curing_specs.pdf) |
| SteakAger | App monitors via Wi-Fi or Bluetooth; "not necessary for the proper functioning". Alarms not described in guide | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| DRY AGER DX 1000 Premium S | None described in the manual. Min/max on the earlier model only [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf) | DOCUMENTED absence |

### 5.4 What is done poorly

- History is thin on standalone controllers: 3 events (Carel) to 10 (XW60K), and an operator can erase it.
- Humidity is excluded from the HACCP history features I could read.
- Alarm thresholds are hidden on cabinets.
- Only an audible alarm in the room is standard. Remote notification needs an add-on on most products.
- App complaints exist: one reseller review says a SteakAger app stopped working after two weeks [S7*](https://www.desertcart.nz/products/137002013) (REPORTED, single anecdote).

### Reference behavior: alarming

- R5.1 The system raises separate alarms for high temperature, low temperature, high humidity, low humidity and door open too long. (DOCUMENTED S1, S16.)
- R5.2 A temperature or humidity condition must persist about 15 min (settable; up to 120 min default on one product) before it alarms. (DOCUMENTED S15, S16, S20.)
- R5.3 Door open alarms after about 5 to 20 min on two products and 60 min on a third. (DOCUMENTED defaults S15, S16, S20.)
- R5.4 Alarms are suppressed around expected disturbances: about 20 min after door close and after defrost, about 90 min after power-on. (DOCUMENTED defaults S15, S16.)
- R5.5 An alarm appears on the display, alternating with the reading, and sounds a buzzer. Any key silences the buzzer, and the visual indication remains until the condition clears. (DOCUMENTED S1, S16, S24.)
- R5.6 Alarms clear on their own when the condition ends (DOCUMENTED S15, S16, S20), on some products with a differential of 1.0 to 2.0 °C or 2.0 %RH (DOCUMENTED S16, S20).
- R5.7 A dry contact can signal alarms externally. (DOCUMENTED S16, S20.)
- R5.8 On equipped models the last 3 to 10 temperature alarm events are stored with date and time, and can be reviewed at the front panel. (DOCUMENTED S17, S20.) Humidity events are not stored in those logs.
- R5.9 Periodic recording is by an add-on device, up to 4 temperatures every 15 min for up to 1 year. (DOCUMENTED S19, excerpt only.)
- R5.10 A maintenance reminder is shown for consumables on a roughly annual cycle. (DOCUMENTED S1.)
- R5.11 Alarm limits are visible and adjustable in controller products (DOCUMENTED S15, S16, S20) and not published in finished-cabinet manuals (S1, S6, S11).

---

## 6. Front-panel usability

### 6.1 Display

| Product | Display | Label |
|---|---|---|
| DX 1000 Premium S | Temperature and humidity actuals in Standard mode, status LEDs. In program mode shows program number and temperature; "Displaying the humidity value is not possible during the SmartAging process". Remaining time shown as value × 10 = hours, with "0.1" for programs with no runtime | DOCUMENTED [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) |
| SteakAger | Readout "shows the set temperature", not actual. Humidity available in the app | DOCUMENTED [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) |
| Klarstein | "LED display is the actual temperature and humidity"; adjustment steps 1 °C / 1 %RH | DOCUMENTED [S11](https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf) |
| XR06CX | 2 digits, red, 14.2 mm | DOCUMENTED [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) |
| XLH360 | 3 digits red 30.5 mm, and 3 digits yellow for lower line; showing cycle state | DOCUMENTED [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf) |
| Carel | 3 digits with sign, decimal, and icons | DOCUMENTED [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1) |
| Stagionello | Touch screen | DOCUMENTED [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf) |

Legibility data (viewing distance, contrast) are not published beyond digit height.

An owner reports the DRY AGER temperature and light controls are behind the door, so you must open it to check or adjust [S8](https://www.nicksolares.com/post/adventures-in-dry-aging-the-arrival) (REPORTED). INFERRED: this opens a door to read a value, adding a disturbance.

### 6.2 Changing a setpoint

- DX 1000 Premium S: hold about 5 s until flashing, arrows, confirm [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). Earlier model: 1 press, change within 10 s, confirm or wait 10 s [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf).
- XR06CX: tap SET to view; hold more than 2 s to edit; confirm by key or 10 s time-out, which stores the value [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf). Parameter time-out 15 s [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Eliwell: 15 s time-out [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf).
- Level separation: user and installer menus; XLH360's Pr2 requires a security code [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). All DOCUMENTED.

Weakness: cryptic 2 to 3 letter parameter codes, hidden menus entered by key chords (XR06CX: SET + key for 3 s, then again 7 s) [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf).

### 6.3 Keylock

- 3 s two-key chord on DX 1000 Premium S (shows "Pof" and "Pon") [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf); XR06CX ("OF" and "on") [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf); XLH360 ("POF") [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf). Carel H6 and remote code [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1). Eliwell LOC blocks setpoint change [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf). All DOCUMENTED.
- Earlier DRY AGER: locked unit can still show setpoint and stored min/max; on/off stays usable [S2](https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf). INFERRED weakness: a locked panel does not stop someone switching regulation off.
- Physical lock on door: DRY AGER, Stagionello [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf).

### 6.4 Programs and recipes

- DRY AGER: "Std" manual mode with subordinate "dry" and "SAL" modes; about 40 numbered programs (P1 to P40), including after-ripening of hams and salami, raw sausage by casing, drying and cleaning, many with a fixed 3 to 56 day runtime [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf). Program setpoints are not published in the manual; they are a black box, and it refers users to a book for detail [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf).
- Stagionello: 30 preset recipes, about 100 recipe memory, user recipes, optional pH probe, phases include salting, stewing, smoking, drying and seasoning [S13](https://argo.webstaurantstore.com/documents/specsheets/curing_specs.pdf) [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf).
- XLH360: one programmable cycle, with dripping, running and stopping phases, timed, then a chosen end action; buzzer and "End" at finish; edits while running are remembered for the next cycle [S16](https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf).
- Carel Universal variant: 5-step cycle [S27](https://digitalassets.reecegroup.com.au/m/d8c71f3b0d81314f/original/Installation-Instructions-CAREL-IR33-Universal-Controllers-Cheat-Sheet.pdf); refrigeration variant has RTC set-point change [S20](https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1).
- SteakAger and Klarstein: no programs. SteakAger has only recommended setpoints [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf).
- Parameter copy: hot key or copy card [S15](https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf) [S24](https://cdn01.remle.com/IMATGES/F029/7000584.pdf).

### 6.5 What is not offered

No product read offers these in its documents: live weight, weight-loss tracking, or a weight-based endpoint; water activity measurement; or product-specific cure tracking to an endpoint. The closest is a remaining-time counter or an optional pH probe [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) [S12](https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf). INFERRED absence: based on documents read, not an exhaustive market survey.

Also: DRY AGER's manual says humidity cannot be displayed during program mode, which means the operator cannot verify a key variable while the most automated mode runs [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf).

### Reference behavior: front panel

- R6.1 In its normal mode the panel shows actual temperature and actual humidity at the same time, with status indicators for cooling, heating, fan, defrost and alarm. (DOCUMENTED S1, S16.)
- R6.2 Temperature digits are about 14 to 30 mm high on panel-mount displays. (DOCUMENTED S15, S16.)
- R6.3 Setpoint viewing takes one short press; changing takes a hold of 2 to 5 s, arrow keys, then confirm, or an automatic commit after about 10 s. (DOCUMENTED S1, S2, S15.)
- R6.4 Menus time out to the normal display after about 15 s with no input. (DOCUMENTED S15, S16, S24.)
- R6.5 A two-key hold of about 3 s locks and unlocks the keypad, with a short on-screen message. While locked, the setpoint remains viewable. (DOCUMENTED S1, S2, S15, S16.)
- R6.6 Installer settings are separated from user settings; one product protects them with a code. (DOCUMENTED S15, S16.)
- R6.7 Numbered or named programs are selectable, from about 30 to 40 presets, with about 100 storable on one product; user recipes on one product. (DOCUMENTED S1, S13.)
- R6.8 Remaining program time is readable from the panel, in one product only in 10 h steps. (DOCUMENTED S1.)
- R6.9 On one product, humidity is not displayed during an automatic program; on another, only the set temperature is displayed. (DOCUMENTED S1, S6.) These are limits of the reference, not targets.
- R6.10 Product weight and weight-loss progress are not shown. (INFERRED absence from documents read.)

---

## 7. Food-safety reference points, and what transfers to curing

### 7.1 Regulatory and technical reference points

The FSIS guideline is written for inspected US establishments. It is the best public statement of what a process must show, and it is not a home-use rule. Labels below are DOCUMENTED by the issuer unless shown.

| Point | Value | Source |
|---|---|---|
| Shelf-stable target | aw ≤ 0.85, "especially toxigenic microorganisms such as S. aureus" | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| aw ≤ 0.85 alone is insufficient | "It is not enough to only meet degree-hours, follow a drying or salt-curing method for Trichinella, and achieve a final water activity" | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Lethality target | 5-log reduction of Salmonella (and STEC where relevant); guideline text shows fermentation and drying alone generally do not achieve it | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| S. aureus during fermentation | Degree-hours measured above 60 °F (15.6 °C) until pH ≤ 5.3; intended to limit growth to 2 logs or less. The limit depends on the highest fermentation temperature; the limit table sits in a separate GMP document that I could not retrieve, so its values are not stated here | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Critical drying parameters | Drying room temperature, drying time, target aw, product characteristics | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Fermentation RH | 85 to 90 % preferred; FSIS still recommends monitoring RH | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Drying RH | "important to maintain"; too fast causes case hardening, too high risks mold; "does not need to be addressed" if no mold issue and weight loss meets targets | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Air flow | Too low fails drying; too high may increase case hardening; same treatment as drying RH | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Product diameter effect | Large casing reached aw 0.85 after about 55 days with 5.0-log E. coli reduction; small casing reached 0.85 in about 17 days with only 2.0 to 3.0 log | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) (citing DeSouza 2018) |
| Moisture:protein | Salami standard of identity MPR 1.9:1 or less | [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) |
| Thermometer accuracy | Ambient air devices ±1.5 °C, or ±3 °F if °F-only (Food Code 4-203.12 as adopted) | [S31](https://lawfilesext.leg.wa.gov/Law/WACArchive/2024/htm/WAC%20246%20-215%20%20CHAPTER/WAC%20246%20-215%20-04222.htm) [S32*](https://regulations.justia.com/states/minnesota/agency-144/chapter-4626/equipment-design-and-construction/part-4626-0560) |
| Growth limits | Typical aw limits: Salmonella 0.94, L. monocytogenes 0.92, S. aureus growth 0.90 under ROP, toxin 0.88; salumi dried to ≤ 0.85 or ≤ 0.91 when S. aureus is controlled | [S33](https://casaofado.wildapricot.org/resources/Documents/Nummer-Charcuterie-CASA-2022.pdf) (conference presentation, not a regulator) |

Water-activity measurement is by a meter: one vendor quotes reading to 0.003 aw and states aw below 0.85 is the FSIS shelf-stable requirement under HACCP Model 10 [S34](https://library.metergroup.com/Application%20Guides/MeatProducts.pdf) (DOCUMENTED by vendor).

### 7.2 Weight loss as an endpoint

- Weight loss is a working yield number and not a safety gate. FSIS treats weight loss as meeting "quality/yield targets" and uses aw as the shelf-stability gate [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf).
- Secondary sources: dry and semi-dry fermented sausage loses about 25 to 50 % weight, aw from below 0.85 to 0.91 [S35*](https://swine.extension.org/dry-and-semi-dry-fermented-and-direct-acidified-sausage-validation) (REPORTED). Hobby guidance: minimum 30 % [S36*](https://dummies.com/article/charcuterie-dry-cured-meats-271056) (REPORTED, cookbook).
- INFERRED: weight loss gives a safe aw only for a given formulation and has to be validated, which is the point of the FSIS diameter example. So a benchmark must not treat a weight-loss target as a safety claim.

### 7.3 Humidity and temperature in documented curing processes

Illustrative lines from FSIS scientific support tables [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf) (REPORTED in the cited studies; DOCUMENTED in guideline):

| Product | Stage | Temperature | RH | Notes |
|---|---|---|---|---|
| Soudjouk-style | Fermentation / drying | 75.2 °F for 72 h / 71.6 °F for 72 h | 90 to 95 / 80 to 85 | Final aw 0.915, plus vacuum storage |
| Basturma (beef) | Curing, then drying | 44 °F (6.7 °C) at 50 % RH, then drying at 70 °F (21 °C) | Cycled 12 h at 65 % and 12 h at 80 % | Final aw 0.87; >5.0-log E. coli O157:H7 but ≥2.0 and <5.0-log Salmonella |
| Biltong | Drying | 77 °F (25 °C) | 55 % | aw 0.90, ≥5.0-log with its specific process |

Cabinet guidance: DRY AGER says about 10 kg is the optimum sausage load for DX 1000 Premium S, and selects "SAL" or programs P26 and P35 to P37 for salami and raw sausage [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED). Charcuterie: SteakAger sets 59 °F, averaging 55 °F, with the fridge removing moisture constantly and the humidifier needed earlier [S6](https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf) (DOCUMENTED). A review says the base SteakAger PRO 40 cannot do charcuterie without add-ons [S38*](https://amazingribs.com/ratings-reviews/steakager-pro-40) (REPORTED). Hobby sources for chambers: 50 to 60 °F, 60 to 80 %RH [S36*](https://dummies.com/article/charcuterie-dry-cured-meats-271056) (REPORTED, not authoritative).

### 7.4 What does and does not transfer

| Benchmark item | Basis | Transfer to curing |
|---|---|---|
| 1.5 °C / 82 %RH defaults | Beef aging | **Does not transfer.** INFERRED from the rows above: curing stages span roughly 7 to 25 °C and 50 to 95 %RH |
| ±2 °C default band | Refrigeration controllers | **Partly.** INFERRED: at a 15 °C setpoint a 2 °C rise crosses the 15.6 °C S. aureus reference, which matters before pH 5.3. A band must be read against the process |
| Fixed fault duty cycle | Refrigeration | **Doubtful.** INFERRED: 15 min on / 30 min off was set for cold rooms; at warm setpoints near room temperature it may overcool. Fails differently from cold storage |
| 90 min post-start alarm exclusion | Refrigeration | **Partly.** INFERRED: fresh loads of sausage release much moisture early, and stage changes are deliberate, so a static window is imperfect |
| Air velocity 0.56 to 2.03 m/s | Beef aging rooms | **Partly.** FSIS warns of case hardening at high flow for dried products [S30](https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf). No curing-specific cabinet velocity is published |
| 40 to 90 %RH range | Cabinet settings | **Upper end falls short.** FSIS prefers 85 to 90 % in fermentation, and one study runs 90 to 95 %. A cabinet capped at 85 % (Klarstein) or with 90 % as its ceiling has little margin at this stage (INFERRED from the figures) |
| Weight loss | None | **Not offered by benchmark products**, and not a safety endpoint |
| Beef reference ranges 0 to 4 °C, 75 to 80 %RH, 28 to 55 d | Review of beef aging [S37*](https://www.ncbi.nlm.nih.gov/pmc/articles/PMC4872334/) | **Does not transfer** |

### 7.5 Limits on what any cabinet can claim

- DRY AGER: no liability for injury from improperly stored goods placed on the market; "Do not consume foods that have expired"; inspect regularly and discard on mold or odor [S1](https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf) (DOCUMENTED).
- No cabinet read claims it validates food safety. Stagionello's "HACCP" is a marketing phrase with no standard, log format or interval given [S13](https://argo.webstaurantstore.com/documents/specsheets/curing_specs.pdf). INFERRED: HACCP is a plan an operator runs, so a cabinet alone cannot meet it.

### Reference behavior: food-safety support

- RF.1 No reference system measures water activity or pH as standard; one offers an optional pH probe. (DOCUMENTED S12.)
- RF.2 No reference system claims that reaching a program end or weight target makes food safe. Duty to inspect stays with the user. (DOCUMENTED S1.)
- RF.3 Ambient temperature indication meets ±1.5 °C if the probe-inclusive figure of about 1 °C holds. (DOCUMENTED S21, S31; the comparison is INFERRED.)
- RF.4 Temperature excursions can be stored with date and time on equipped models; humidity usually is not. (DOCUMENTED S17, S20.)
- RF.5 For a process record, the parameters a regulator expects are drying room temperature, drying time, target water activity and product characteristics; humidity and air flow are expected to be understood but need less formal handling. (DOCUMENTED S30.)

---

## 8. Sources

Accessed 2026-11-06. `*` = search-result excerpt only; page refused fetch.

| ID | Source | URL |
|---|---|---|
| S1 | DRY AGER DX 1000 Premium S user manual (2021) | https://www.dry-ager.com/wp-content/uploads/2021_DryAger_DX1000_PS_Gebrauchsanweisung_UserManual_Screen.pdf |
| S2 | DRY AGER DX 1000 Bedienungsanleitung (German, 2017) | https://www.dry-ager.com/wp-content/uploads/DRYAGER_DX1000_Bedienungsanleitung.pdf |
| S3 | DRY AGER UX 1500 Pro S use and care manual | https://www.webstaurantstore.com/documents/pdf/dryager_use_and_care_manual_ux1500_pro_s.pdf |
| S4 | DRY AGER UX 0750 Pro spec sheet | https://www.webstaurantstore.com/documents/specsheets/td_ux0750p_2009_1.pdf |
| S5 | Metos reseller page, DRY AGER DX 500 | https://www.metos.fi/en/p/meat-maturing-fridge-metos-dry-ager-dx-500-p_mg4620018 |
| S6 | SteakAger PRO 20/40 user guide | https://media.cmsmax.com/ux2uxzn70229exooou1wq/new-steakager-pro-20-40-manual-online.pdf |
| S7* | SteakAger reseller listing with buyer reviews (desertcart regional page; which regional page carried the review was not pinned down) | https://www.desertcart.nz/products/137002013 |
| S8 | Nick Solares, Adventures in Dry Aging, owner blog | https://www.nicksolares.com/post/adventures-in-dry-aging-the-arrival |
| S9* | Smoking Meat Forums, UX 1000 Dry Ager thread | https://www.smokingmeatforums.com/threads/ux-1000-dry-ager.328606/ |
| S10* | Smoking Meat Forums, Caso 125 dry ager issues | https://www.smokingmeatforums.com/threads/caso-125-dry-ager-issues.323896 |
| S11 | Klarstein Steakhouse Pro manual 10046386 | https://res.cloudinary.com/chal-tec/image/upload/bbg/10046386/bda/10046386_BDA_DE-EN-ES-FR-IT.pdf |
| S12 | Omcan Combo Stagionello + Maturmeat spec sheet | https://cdn.restaurantsupply.com/pdf/a/0/6/a06f3120-53ce-4754-8da5-ffaaefe63789.pdf |
| S13 | Omcan Stagionello curing cabinets spec sheet | https://argo.webstaurantstore.com/documents/specsheets/curing_specs.pdf |
| S14* | Zanduco listing, Stagionello Twin 100+100 kg | https://www.zanduco.com/us/omcan-stagionello-twin-100-100-kg.html |
| S15 | Dixell XR06CX operating manual, r1.2, 07.11.07 | https://media.caterboss.ie/files/HVF11B/Unifrost_Dixell_XR06CX_Controller_Manual_9510e3dcc417.pdf |
| S16 | Dixell XLH360 operating instructions, r1.0, 15.04.2005 | https://arbo.it/content/product_assets/4f/24/4f241f3e9b23f4d8b61f870bcb476fcf/schede_tecniche/xhl360_scheda_tecnica.pdf |
| S17 | Cabinet builder manual, H.A.C.C.P control panel Dixell XW60K (GGM copy) | https://static.ggmgastro.com/K%C3%BChlung/kuehlschrank/ksf/Anleitung%20altes%20Display.pdf |
| S18 | Dixell XW60K manual, partial via ManualsLib excerpt (includes third-party FAQ) | https://www.manualslib.com/manual/1665451/Dixell-Xw60k.html |
| S19* | Copeland/Dixell Cold Chain Solutions brochure | https://webapps2.copeland.com/ci/dixell-xweb/2018ECT-59%20Dixell%20Cold%20Chain%20Solutions.pdf |
| S20 | Carel ir33 platform manual +030220441, rel. 2.5, 16.06.10 | https://ish.carel.com/documents/10191/0/%2B030220441/2785a260-cf4f-4fa9-9956-bb208e305eef?version=1.1 |
| S21 | Carel ir33 technical leaflet +050003061 | https://ish.carel.com/documents/10191/0/%2B050003061/672869f3-306b-46d6-881d-e9f22156cc65?version=1.0 |
| S22 | Carel ir33 series product page | https://ish.carel.com/ir33-series/-/journal_content/56_INSTANCE_i4q5KIMLInKK/10191/25234 |
| S23 | Carel IR32M manual, compressor parameters page | https://www.manualslib.com/manual/1649829/Carel-Ir32m.html?page=20 |
| S24 | Eliwell IDW961/971/974 manual (Spanish) | https://cdn01.remle.com/IMATGES/F029/7000584.pdf |
| S25* | Eliwell ID 974 manual listing | https://manualmachine.com/eliwell/id974/21475188-user-manual |
| S26* | Intarcon copy of Dixell XW270K manual | https://www.intarcon.com/pdfs/EN/XW270K.pdf |
| S27 | Carel IR33 Universal parameter summary (Reece) | https://digitalassets.reecegroup.com.au/m/d8c71f3b0d81314f/original/Installation-Instructions-CAREL-IR33-Universal-Controllers-Cheat-Sheet.pdf |
| S28* | Lancaster et al. 2022, Assessment of dry-aged beef from commercial aging locations across the United States, Int J Gastron Food Sci 27, doi:10.1016/j.ijgfs.2022.100466 | https://academica-e.unavarra.es/handle/2454/42679 |
| S29 | Bulgaru et al. 2022, Foods 11:1526 | https://mdpi-res.com/d_attachment/foods/foods-11-01526/article_deploy/foods-11-01526.pdf |
| S30 | USDA FSIS, Ready-to-Eat Fermented, Salt-Cured, and Dried Products Guideline, FSIS-GD-2023-0002, 5 May 2023. Origin refused automated fetch; read via web.archive.org copy of this URL | https://www.fsis.usda.gov/sites/default/files/media_file/documents/FSIS-GD-2023-0002.pdf |
| S31 | Washington WAC 246-215-04222 (Food Code 4-203.12) | https://lawfilesext.leg.wa.gov/Law/WACArchive/2024/htm/WAC%20246%20-215%20%20CHAPTER/WAC%20246%20-215%20-04222.htm |
| S32* | Minnesota Rule 4626.0560 (Food Code 4-203.12) | https://regulations.justia.com/states/minnesota/agency-144/chapter-4626/equipment-design-and-construction/part-4626-0560 |
| S33 | Nummer, Charcuterie, CASA presentation, 2022 | https://casaofado.wildapricot.org/resources/Documents/Nummer-Charcuterie-CASA-2022.pdf |
| S34 | METER Group, meat products application guide | https://library.metergroup.com/Application%20Guides/MeatProducts.pdf |
| S35* | Extension Foundation, dry and semi-dry fermented sausage validation | https://swine.extension.org/dry-and-semi-dry-fermented-and-direct-acidified-sausage-validation |
| S36* | Charcuterie For Dummies excerpt, dry-cured meats | https://dummies.com/article/charcuterie-dry-cured-meats-271056 |
| S37* | Dry aging of beef; Review, J Anim Sci Technol | https://www.ncbi.nlm.nih.gov/pmc/articles/PMC4872334/ |
| S38* | AmazingRibs, SteakAger PRO 40 review | https://amazingribs.com/ratings-reviews/steakager-pro-40 |
| S39 | DRY AGER UX 1500 Pro spec sheet | https://www.webstaurantstore.com/documents/specsheets/td_ux1500p_2102_1_1_.pdf |

## 9. Evidence quality by area

| Area | Evidence | Comment |
|---|---|---|
| 1. Control stability | **Weak** for achieved bands, moderate for capability | Only stated bands, no logged data |
| 2. Recovery | **Weakest** | No recovery time published; alarm windows used as proxy |
| 3. Equipment protection | **Strongest** | Parameter tables, four controller families |
| 4. Failure behavior | Good for controllers, thin for cabinets | DRY AGER alarm table is explicit; others silent |
| 5. Alarming | Good for controllers; thresholds not published on cabinets | HACCP logging detailed only for two |
| 6. Front panel | Moderate | Documents describe procedures; legibility not published |
| 7. Food safety | Strong | FSIS guideline read directly, through archive copy |
