<!--
  CANONICAL SOURCE — do not edit here.

  This is a verbatim mirror of the approved PRD v1.0.0, kept in both repositories so a developer
  who cloned only one of them still has the requirement they are building against. If the PRD
  changes, update both copies in the same change and bump the version line below.

  Requirements are cited throughout the sprint documents by PRD section (e.g. "PRD §3.3") and by
  the stable requirement IDs assigned in `requirements-register.md`.
-->

# Product Requirements Document (PRD): BlindPulse Replay Lab
**Version:** 1.0.0  
**Status:** Approved / Design Complete  
**Target Platforms:** Desktop Web & Mobile Responsive Web (Mobile PWA / Native Ready)  
**Primary Aesthetic / Design Systems:** `Terminal Precision` (Dark Mode) & `Institutional Precision Replay` (Light Mode)

---

## 1. Executive Summary & Vision

### 1.1 The Core Problem
Most retail and aspiring quantitative traders fail because of **hindsight bias**, **curve fitting**, and **psychological tilt during drawdowns**. Traditional backtesting tools (e.g., standard TradingView Bar Replay) present known assets and visible dates, which subconsciously biases the trader's execution (e.g., "I know Bitcoin broke out in late 2020, so I will only look for longs"). Furthermore, when traders blow up or reset an account, they lose all historical trade data, execution mistakes, and lessons learned.

### 1.2 The BlindPulse Solution
**BlindPulse Replay Lab** is a high-precision, blinded market replay and execution simulator designed to build raw technical edge and emotional resilience through:
1. **Zero-Hindsight Blinded Feeds**: Ticker symbols, historical dates, and volume cues are completely masked with synthetic identifiers (e.g., `Asset #842`) until the session is voluntarily unblinded.
2. **Deterministic Tick-Level Execution**: Traders execute longs, shorts, limit orders, and bracket orders with realistic spread and slippage simulation.
3. **Non-Destructive Account Reset Trees**: When an account reaches its maximum drawdown or the trader elects to reset, the balance resets into a new branched iteration (e.g., `Iteration 04`), while permanently archiving previous iterations (`#01` to `#03`) so the trader never loses their cryptographic execution ledger.
4. **Behavioral & Quantitative Audit**: Post-session trade journals reveal real-world historical market events (e.g., "SVB Collapse Week"), compare system return against buy-and-hold alpha, and grade execution discipline.

---

## 2. Target Personas

| Persona | Motivation & Goals | Key Pain Points in Other Tools |
| :--- | :--- | :--- |
| **Systematic Discretionary Trader** | Wants to practice Price Action, ICT/Order Flow, or Market Structure without cherry-picked charts. | Can't un-see the ticker or year; knows how big macro events ended. |
| **Prop Firm Candidate** | Needs to practice strict risk management (max 5% daily drawdown, 1:2 min R:R) in a live-pressure environment. | Accidental rule violations reset progress without tracking where psychology failed. |
| **Quantitative / Algo Researcher** | Seeks to calibrate execution rules across thousands of anonymized market regimes (Equities, FX, Crypto, Commodities). | Lack of granular R-multiple distributions and session-specific alpha analytics. |

---

## 3. Core System Architecture & Features

### 3.1 Blinded Replay Engine (`Replay Terminal`)
- **Randomized Slicing**: Dynamically samples from an archive of 1,200+ historical cycles (2008–2025) across FX Majors, Equity Indices, Commodities, and Crypto.
- **Masking Protocol**: 
  - Price values are normalized or offset to prevent price-level identification.
  - X-axis date stamps are replaced with relative tick offsets (`T - 140`, `T - 0 Live Playhead`, `Future Window`).
  - Ticker name is masked as `Asset #XXX [FX/Crypto Masked]`.
- **Playback Controls**:
  - Step Backward / Forward candle-by-candle (Keyboard shortcut: `Spacebar`).
  - Continuous replay at variable speeds (`0.5x`, `1.0x`, `3.0x`, `5.0x`, `10.0x`).
  - Tick progress indicator (e.g., `142 / 500 bars scanned`).
  - Safety-gated **Reveal Asset** trigger.

### 3.2 TradingView-Style Technical Analysis Ribbon
- **Trendline Suite**: Freehand Trendline, Horizontal Line, Horizontal Ray, Extended Line, Vertical Line.
- **Fibonacci & Projections**: Fibonacci Retracement (0.00%, 0.236, 0.382, 0.500 Equilibrium, 0.618 Golden Pocket, 0.786, 1.000), Trend-based Extensions.
- **Zone & Geometry Tools**: Order Block / Supply & Demand Box (with customizable shading), Polyline, Brush, Annotation Note.
- **Risk / Position Brackets**: Interactive on-chart Long/Short brackets showing real-time Target Pips, Stop Loss Risk ($ and %), and live Risk-to-Reward (R:R) ratio.
- **Sub-Chart Indicators**: RSI (14) with Overbought/Oversold thresholds, Dual EMAs (20/50/200), MACD momentum histogram, and Volume profile.
- **Scale Controls**: Logarithmic (`LOG`), Auto (`AUTO`), and Percentage (`%`) scale modes.

### 3.3 Execution & Order Management Dock
- **Order Types**: Market, Limit, Stop with instant lot sizing or % equity risk calculations.
- **Pre-Entry Rule Verification**:
  - Max Daily Drawdown check (<5.0%).
  - Mandatory hard Stop-Loss assignment prior to order submission.
  - Minimum R:R filter (>1:2.00).
- **Position Actions**: Quick Breakeven (`Set BE`), Scale 50% (`Close 50%`), and emergency `Close All`.

### 3.4 Post-Session Mystery Reveal & Trade Journal
- **Mystery Unblind**: Reveals the actual ticker (e.g., `EUR/USD`), simulated real timeframe (`15-Minute Candles`), and real-world date window (e.g., `March 14–17, 2023 · SVB Contagion`).
- **Benchmark Alpha**: System Performance vs. Buy-and-Hold Outperformance calculation.
- **Behavioral Discipline Index (0–100)**: Flags impulsive FOMO entries, revenge trades, early take-profit cuts, and adherence to planned stop-losses.
- **Macro Driver Annotation**: Educational breakdown explaining the macroeconomic and fundamental context of that historical replay period.

### 3.5 Account Hierarchy & Resets (`Branch & Reset Tree`)
- **Immutable Ledger**: Resets never overwrite past trades. Instead, every reset initiates a new child branch (`Iteration 04`) while freezing past iterations (`#01`, `#02`, `#03`) in read-only state.
- **Cross-Iteration Equity Curve Overlay**: Visualizes comparative performance curves across all runs to measure learning curve and consistency.
- **Forking Capabilities**: Ability to clone specific strategy parameters from a previous successful iteration.

### 3.6 Authentication & Institutional Access
- **Trading SSO**: Direct Single Sign-On with TradingView accounts.
- **Developer / Standard SSO**: Google, GitHub, Apple OAuth.
- **Enterprise SAML 2.0 / Okta**: Tailored for prop firm desks and trading academies.
- **Instant Sandbox Mode**: Direct unauthenticated sandbox gateway for quick trial replays.

---

## 4. Screen Inventory & Platform Deliverables

| Screen Name | Target Device | Design Theme | Primary Purpose |
| :--- | :--- | :--- | :--- |
| **Replay Terminal (Dark)** | Desktop | Terminal Precision (Dark) | Primary charting canvas, TA tools, live playback, order execution. |
| **Replay Terminal (Light)** | Desktop | Institutional Precision (Light) | High-contrast trading floor charting and drawing experience. |
| **Trade Journal & Reveal (Dark)** | Desktop | Terminal Precision (Dark) | Post-session unblinding, execution footprint, macro retrospective. |
| **Trade Journal & Reveal (Light)**| Desktop | Institutional Precision (Light) | High-clarity light mode post-mortem review and behavioral audit. |
| **Accounts & Resets (Dark)** | Desktop | Terminal Precision (Dark) | Branching reset tree, multi-iteration trajectory comparison. |
| **Accounts & Resets (Light)** | Desktop | Institutional Precision (Light) | Clean tabular ledger of sub-account iterations and discipline logs. |
| **Performance Analytics (Dark)** | Desktop | Terminal Precision (Dark) | Sharpe, R-Multiple distribution, session alpha, win/loss clusters. |
| **Performance Analytics (Light)**| Desktop | Institutional Precision (Light) | Quantitative deck with risk waterline and expectancy modeling. |
| **Login & SSO Authentication** | Desktop | Terminal Precision (Dark) | TradingView SSO, WebAuthn keys, Enterprise Okta gateway. |
| **Mobile Replay Terminal** | Mobile (390px) | Terminal Precision (Dark) | Thumb-friendly candlestick chart, touch drawing tools, playback dock. |
| **Mobile Trade & Execution** | Mobile (390px) | Terminal Precision (Dark) | Dedicated order slip, risk sizing presets, active positions table. |
| **Mobile Journal & Reveal** | Mobile (390px) | Terminal Precision (Dark) | Card-based mystery unblinding, macro context tape, trade fills. |
| **Mobile Accounts & Resets** | Mobile (390px) | Terminal Precision (Dark) | Mobile branch switcher, active balance recovery meter. |

---

## 5. Design System Tokens & Brand Standards

### 5.1 Dark Theme (`Terminal Precision`)
- **Canvas / Base Surface**: `#010F1F` / `#051424` / `#0B0E14`
- **Elevated Cards / Docks**: `#0D1C2D` / `#132235` (Border: `#1E293B`)
- **Bullish / Profit / Accent**: `#00F0A8` (Emerald High-Vibrancy)
- **Bearish / Loss / Drawdown**: `#FF4D6D` (Vivid Coral Crimson)
- **Playback / Tech Accent**: `#00D2FF` (Electric Cyan)
- **Typography**: Inter (UI and Headings) + Monospace JetBrains / SF Mono (Prices, PnL, Ticks)

### 5.2 Light Theme (`Institutional Precision Replay`)
- **Canvas / Base Surface**: `#FAFBFF` / `#FFFFFF`
- **Card Containers**: `#F1F5F9` / `#E2E8F0` (Border: `#CBD5E1`)
- **Bullish / Profit**: `#059669` (Deep Forest Emerald)
- **Bearish / Drawdown**: `#E11D48` (Institutional Crimson)
- **Primary Brand**: `#0284C7` (Cobalt Precision Blue)

---

## 6. Technical & Functional Non-Functional Requirements (NFRs)

1. **Latency & Execution**: Simulated tick latency displayed on UI (<15ms) to mirror institutional market feed fidelity.
2. **Chart Rendering Performance**: Candlestick canvas must sustain 60 FPS during fast 10x replay mode with multiple vector overlays (EMAs, Fibonacci, Demand Zones).
3. **Data Privacy & Cryptographic Logging**: All replay actions, fills, and psychological tags are locally indexed with deterministic session hashes (`ROOT_HASH: 0x8f7...a19c`) ensuring authentic proof-of-skill.
4. **Responsive Adaptability**: Full touch gestures (pinch-to-zoom, drag crosshairs, tap-to-step) for mobile web and PWA wrappers.

---

## 7. Roadmap & Next Steps
- **Phase 1 (Current Milestone)**: Complete UI/UX Specification, Design Systems, and Screen Suite (Desktop + Mobile, Dark + Light).
- **Phase 2**: Integration with real market tick database (G10 FX, CME Futures, Binance Spot historical datasets).
- **Phase 3**: Automated Prop Firm Challenge Simulator Mode with custom max drawdown and consistency rule enforcement.
- **Phase 4**: Trader Edge Sharing Cards (Sharable social graphics showing blinded execution vs. actual market aftermath).
