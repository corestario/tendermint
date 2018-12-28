# Benchmarks and load test

## Setup
There're 2 benchmarks: max and rps. They are made in `quick and dirty` way just to check Tentermint properties and possible issues.
* `max` is for testing maximum possible RPS
* `rps` is for testing on stable load

Test config file /tendermint/bench/config.toml The setup is configured to have a block per ~2sec.

## Run tests
`make build`
`make build-docker`
`sudo rm -Rdf build/node*`
`export TESTNET_NODES=10 && make localnet-start`
`go run ./bench/bench.go -server="http://142.93.184.168:26657" -writetxs=80 &> bench.txt`

## Issues
Tendermint definitely has a few memory leaks. OOM killer takes a node after 1-6 hours.

## Logs
Logs and profiles can be found /tendermint/bench/logs

## Results
| TXs | Total time | RPS |
| --- | ---------- | --- |
| 20000 | 2.49s | 8033 |
| 50000 | 9.60s | 5210 |

## Hardware
```Architecture:        x86_64
CPU op-mode(s):      32-bit, 64-bit
Byte Order:          Little Endian
CPU(s):              8
On-line CPU(s) list: 0-7
Thread(s) per core:  2
Core(s) per socket:  4
Socket(s):           1
NUMA node(s):        1
Vendor ID:           GenuineIntel
CPU family:          6
Model:               158
Model name:          Intel(R) Core(TM) i7-7700HQ CPU @ 2.80GHz
Stepping:            9
CPU MHz:             900.036
CPU max MHz:         3800,0000
CPU min MHz:         800,0000
BogoMIPS:            5616.00
Virtualisation:      VT-x
L1d cache:           32K
L1i cache:           32K
L2 cache:            256K
L3 cache:            6144K
NUMA node0 CPU(s):   0-7
Flags:               fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc art arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc cpuid aperfmperf tsc_known_freq pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 sdbg fma cx16 xtpr pdcm pcid sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand lahf_lm abm 3dnowprefetch cpuid_fault epb invpcid_single pti ssbd ibrs ibpb stibp tpr_shadow vnmi flexpriority ept vpid fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid mpx rdseed adx smap clflushopt intel_pt xsaveopt xsavec xgetbv1 xsaves dtherm ida arat pln pts hwp hwp_notify hwp_act_window hwp_epp flush_l1d
```

```
RAM 48GB

Handle 0x003F, DMI type 17, 40 bytes
Memory Device
	Array Handle: 0x003E
	Error Information Handle: Not Provided
	Total Width: 64 bits
	Data Width: 64 bits
	Size: 16384 MB
	Form Factor: SODIMM
	Set: None
	Locator: ChannelA-DIMM0
	Bank Locator: BANK 0
	Type: DDR4
	Type Detail: Synchronous
	Speed: 2400 MT/s
	Manufacturer: Kingston
	Serial Number: 1A2AC153
	Asset Tag: 9876543210
	Part Number: MSI24D4S7D8MB-16    
	Rank: 2
	Configured Clock Speed: 2133 MT/s
	Minimum Voltage: 1.2 V
	Maximum Voltage: 1.2 V
	Configured Voltage: 1.2 V

Handle 0x0040, DMI type 17, 40 bytes
Memory Device
	Array Handle: 0x003E
	Error Information Handle: Not Provided
	Total Width: 64 bits
	Data Width: 64 bits
	Size: 16384 MB
	Form Factor: SODIMM
	Set: None
	Locator: ChannelA-DIMM1
	Bank Locator: BANK 1
	Type: DDR4
	Type Detail: Synchronous
	Speed: 2400 MT/s
	Manufacturer: Kingston
	Serial Number: 6A172167
	Asset Tag: 9876543210
	Part Number: KHX2400C15S4/16G    
	Rank: 2
	Configured Clock Speed: 2133 MT/s
	Minimum Voltage: 1.2 V
	Maximum Voltage: 1.2 V
	Configured Voltage: 1.2 V

Handle 0x0041, DMI type 17, 40 bytes
Memory Device
	Array Handle: 0x003E
	Error Information Handle: Not Provided
	Total Width: Unknown
	Data Width: Unknown
	Size: No Module Installed
	Form Factor: Unknown
	Set: None
	Locator: ChannelB-DIMM0
	Bank Locator: BANK 2
	Type: Unknown
	Type Detail: None
	Speed: Unknown
	Manufacturer: Not Specified
	Serial Number: Not Specified
	Asset Tag: Not Specified
	Part Number: Not Specified
	Rank: Unknown
	Configured Clock Speed: Unknown
	Minimum Voltage: Unknown
	Maximum Voltage: Unknown
	Configured Voltage: Unknown

Handle 0x0042, DMI type 17, 40 bytes
Memory Device
	Array Handle: 0x003E
	Error Information Handle: Not Provided
	Total Width: 64 bits
	Data Width: 64 bits
	Size: 16384 MB
	Form Factor: SODIMM
	Set: None
	Locator: ChannelB-DIMM1
	Bank Locator: BANK 3
	Type: DDR4
	Type Detail: Synchronous
	Speed: 2400 MT/s
	Manufacturer: Kingston
	Serial Number: 6C172167
	Asset Tag: 9876543210
	Part Number: KHX2400C15S4/16G    
	Rank: 2
	Configured Clock Speed: 2133 MT/s
	Minimum Voltage: 1.2 V
	Maximum Voltage: 1.2 V
	Configured Voltage: 1.2 V
```

```DISK https://www.samsung.com/ru/memory-storage/ssd/MZ-V6P1T0BW/```
