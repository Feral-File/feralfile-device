# FFX1 Device life cycle

## App flows

### App startup

```mermaid
flowchart TD
    FF1Start[FF1 Start] --> Bluetooth(Start Bluetooth)
    Bluetooth --> HasInternet(Has Internet)

    HasInternet --> |No| QRCode1(Display QRCode)
    HasInternet --> |Yes| UpToDate1{Up to date}

    UpToDate1 --> |No| Update(Update to latest version)
    UpToDate1 --> |Yes| Paired{Has paired<br/>with mobile app}
    Update --> |Restart| FF1Start
    Paired --> |No| QRCode2(Display QRCode)
    Paired --> |Yes| Artwork(Artwork Playback)
    QRCode2 --> |Connect bluetooth<br/>Command: keep_wifi| Relayer1(Get relayer credential<br/>Return keep_wifi)
    Relayer1 --> Artwork

    QRCode1 --> |Internet<br/>Detected| HasInternet
    QRCode1 --> |Connect bluetooth<br/>Command: connect_wifi| UpToDate2{Up to date}
    UpToDate2 --> |No| Update
    UpToDate2 --> |Yes| Relayer2(Get relayer credential<br/>Return connect_wifi)
    Relayer2 --> Artwork
```

### App update

```mermaid
flowchart TD
    Current[Current Version] --> Trouble{Having<br/>trouble}

    Trouble --> |Yes| Rollback{Choose version<br/>to rollback}
    Trouble --> |No| Update[Update at 3am]

    Update --> |Restart| Latest

    Rollback --> |Fresh| FactoryVersion(Factory Version)
    Rollback --> LastVersion(Last Version)

    FactoryVersion --> |Force Update| Latest
    LastVersion --> |Force Update| Latest
```

### Command Processing Flow

```mermaid
flowchart TD
    %% Command Flow from Mobile
    Mobile[Mobile Controller] --> |Send Command| Relayer[Relayer Service]
    Relayer --> |WebSocket Message| Connectd[Connectd Service]
    
    %% Command Processing
    Connectd --> Mediator[Mediator]
    Mediator --> |Parse Message| Parse{Message Type}
    
    Parse --> |System Message| System[Handle System Message<br/>Save Topic ID]
    Parse --> |Command Message| CmdType{Command Type}
    
    CmdType --> |Device Command| DeviceCmd[Command Handler<br/>Execute Device Commands]
    CmdType --> |Web Command| WebCmd[Chrome DevTools Protocol]
    
    DeviceCmd --> |Available Commands| Commands[Device Commands:<br/>• connect<br/>• showPairingQRCode<br/>• deviceMetrics<br/>• sendKeyboardEvent<br/>• dragGesture<br/>• tapGesture<br/>• rotate<br/>• shutdown<br/>• getDeviceStatus<br/>• updateToLatestVersion]
    
    WebCmd --> |Forward to Browser| Browser[Chromium Browser]
    Browser --> |Execute JavaScript| WebApp[Web Application]
    
    %% Response Flow
    Commands --> |Return Result| Response[Send Response]
    WebApp --> |Return Result| Response
    
    Response --> |RPC Response| Relayer
    Relayer --> |WebSocket Response| Mobile
    
    %% Error Handling
    DeviceCmd --> |Error| Error[Error Response]
    WebCmd --> |Error| Error
    Error --> Response
    
    class Connectd,Relayer service
    class Mediator,Commands,Browser,WebApp component
    class Parse,CmdType decision
    class Mobile external
```

## Telemetry (Heartbeat)

All the events should only consider network connected scenario otherwise it can't be sent over heartbeat.

### Device status
| Field | Type | Description |
| :--- | :--- | :--- |
| `Timestamp` | DateTime | The ISO 8601 timestamp (UTC) when the heartbeat was generated. |
| `MAC Address` | String | The unique, immutable MAC address of the network interface. |
| `Build` | String | The current firmware build version (e.g., "develop-0.0.1"). |
| `Screen Info` | String | String detailing screen status (e.g., "1920x1080@60"). |
| `CPU Temp` | Number | The core CPU temperature in Celsius (°C). **Alerts if > 55.** |
| `CPU Usage` | Percent | The current CPU utilization (0.00 to 1.00). **Alerts if > 0.80.** |
| `GPU Usage` | Percent | The current GPU utilization (0.00 to 1.00). **Alerts if > 0.80.** |
| `Memory Usage`| Percent | The percentage of total RAM currently in use (0.00 to 1.00). **Alerts if > 0.80.** |
| `Disk Usage` | Percent | The percentage of total disk storage currently in use (0.00 to 1.00). **Alerts if > 0.80.** |
| `Uptime` | String | The duration the device has been running since last boot, in "D H:M:S" format. |
| `Status` | String | **(Calculated)** "✅ Online" or "❌ Offline". Derived in the spreadsheet, not sent by device. |
| `Public Key` | String | The device's public key for signature verification. |
| `Signature` | String | The payload's cryptographic signature for data integrity. |
| `Page` | String | The setupd page state. |
| `Page Uptime` | String | The duration the setupd has been staying under this state page, in "D H:M:S" format. |

### Setupd pages

| Name | Notes |
| :- | :- |
| `QRCode` | QR code displayed for pairing setup. Network already connected. |
| `FactoryReset` | Device initiated rollback to factory version. |
| `SystemUpgrade` | Force firmware update initiated |
| `WebApp` | Artwork playback has begun. |

## Version control

We deploy the firmware versions through 2 main channels:
- Dev channel: https://feralfile-device-distribution.bitmark-development.workers.dev/
- Prod channel: https://x1.feral-file.workers.dev/

Our versioning follow Semantic Versioning format.

Each channel has API to specific min_version and latest_version. If the current version on the device is older than min_version, it's forced to update. Otherwise, it will update to the latest version silently at 3am.