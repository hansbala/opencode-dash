import SwiftUI
import AppKit
import Foundation
import Combine

// MARK: - Store

@MainActor
final class UsageStore: ObservableObject {
    static let shared = UsageStore()

    @Published private(set) var snapshot: UsageSnapshot?
    @Published private(set) var error: String?
    @Published private(set) var loading = false
    @Published var now = Date()

    private let client = OpenCodeClient()
    private var started = false

    var limits: LimitsInfo? { snapshot?.limits }
    var stats: StatsInfo? { snapshot?.stats }
    var lastUpdated: Date? { snapshot?.fetchedAt }
    var percent: Double { snapshot?.percent ?? 0 }
    var percentLabel: String { snapshot == nil ? "—" : "\(Int(percent.rounded()))%" }
    var resetDate: Date? { snapshot?.resetDate }
    var cycleStart: Date? { snapshot?.cycleStart }
    var pacePercent: Double? { snapshot?.pacePercent }
    var dailyAverage: Double? { snapshot?.dailyAverage }
    var chartPoints: [ChartPoint] { snapshot?.chartPoints ?? [] }

    var accent: Color {
        if percent >= 90 { return .red }
        if percent >= 70 { return .orange }
        return Color(red: 0.20, green: 0.78, blue: 0.45)
    }

    func start() {
        guard !started else { return }
        started = true
        Task { await loop() }
    }

    private func loop() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: .seconds(60))
        }
    }

    func refresh() async {
        guard !loading else { return }
        loading = true
        defer { loading = false }

        do {
            snapshot = try await client.snapshot()
            error = nil
        } catch {
            self.error = (error as NSError).localizedDescription
        }
    }
}

// MARK: - Views

private struct Card<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            content
        }
        .padding(12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .fill(Color.primary.opacity(0.045))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10, style: .continuous)
                .stroke(Color.primary.opacity(0.07), lineWidth: 1)
        )
    }
}

private struct LimitRow: View {
    let name: String
    let window: LimitWindow
    let color: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(name).font(.system(size: 12, weight: .medium))
                Spacer()
                Text("Resets in \(countdown(to: window.resetDate))")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                Text("\(Int(window.percent.rounded()))% used")
                    .font(.system(size: 11, weight: .medium))
                    .monospacedDigit()
                    .frame(width: 58, alignment: .trailing)
            }
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.primary.opacity(0.10))
                    Capsule()
                        .fill(color)
                        .frame(width: max(3, geo.size.width * min(1, window.percent / 100)))
                }
            }
            .frame(height: 5)
        }
    }
}

private struct UsageChart: View {
    let points: [ChartPoint]
    let forecast: Double?
    let totalDays: Double
    let color: Color

    private let maxPercent: Double = 150

    var body: some View {
        GeometryReader { geo in
            let width = geo.size.width
            let height = geo.size.height
            let x: (Double) -> CGFloat = { day in
                CGFloat(min(totalDays, max(0, day)) / totalDays) * width
            }
            let y: (Double) -> CGFloat = { percent in
                height - CGFloat(min(maxPercent, max(0, percent)) / maxPercent) * height
            }

            ZStack(alignment: .topLeading) {
                // Grid
                ForEach([0.0, 50.0, 100.0, 150.0], id: \.self) { value in
                    Path { path in
                        path.move(to: CGPoint(x: 0, y: y(value)))
                        path.addLine(to: CGPoint(x: width, y: y(value)))
                    }
                    .stroke(Color.primary.opacity(value == 100 ? 0.16 : 0.06),
                            style: StrokeStyle(lineWidth: 1, dash: value == 100 ? [3, 3] : []))
                }

                // Area under the actual line
                if points.count > 1 {
                    Path { path in
                        path.move(to: CGPoint(x: x(points[0].day), y: height))
                        for point in points {
                            path.addLine(to: CGPoint(x: x(point.day), y: y(point.percent)))
                        }
                        path.addLine(to: CGPoint(x: x(points.last!.day), y: height))
                        path.closeSubpath()
                    }
                    .fill(
                        LinearGradient(
                            colors: [color.opacity(0.28), color.opacity(0.02)],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )
                }

                // Actual usage
                if points.count > 1 {
                    Path { path in
                        path.move(to: CGPoint(x: x(points[0].day), y: y(points[0].percent)))
                        for point in points.dropFirst() {
                            path.addLine(to: CGPoint(x: x(point.day), y: y(point.percent)))
                        }
                    }
                    .stroke(color, style: StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                }

                // Forecast
                if let forecast, let last = points.last {
                    Path { path in
                        path.move(to: CGPoint(x: x(last.day), y: y(last.percent)))
                        path.addLine(to: CGPoint(x: width, y: y(forecast)))
                    }
                    .stroke(
                        Color(red: 0.98, green: 0.75, blue: 0.25),
                        style: StrokeStyle(lineWidth: 1.5, lineCap: .round, dash: [4, 3])
                    )

                    Circle()
                        .fill(Color(red: 0.98, green: 0.75, blue: 0.25))
                        .frame(width: 5, height: 5)
                        .position(x: width, y: y(forecast))

                    Text("\(Int(forecast.rounded()))%")
                        .font(.system(size: 9, weight: .semibold))
                        .foregroundStyle(Color(red: 0.98, green: 0.75, blue: 0.25))
                        .position(x: width - 18, y: max(8, y(forecast) - 10))
                }

                // Today marker
                if let last = points.last {
                    Circle()
                        .fill(color)
                        .frame(width: 6, height: 6)
                        .position(x: x(last.day), y: y(last.percent))
                }
            }
        }
        .frame(height: 94)
    }
}

struct PopoverView: View {
    @ObservedObject var store: UsageStore
    private let tick = Timer.publish(every: 1, on: .main, in: .common).autoconnect()

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            chart
            limitsCard
            modelsCard
            footer
        }
        .padding(16)
        .frame(width: 360)
        .background(Color(nsColor: .windowBackgroundColor))
        .onReceive(tick) { _ in
            store.now = Date()
        }
        .task { await store.refresh() }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Go usage")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(.secondary)
                    Text(store.percentLabel)
                        .font(.system(size: 32, weight: .semibold))
                        .monospacedDigit()
                        .foregroundStyle(store.limits == nil ? .secondary : .primary)
                }
                Spacer()
                Image(systemName: "arrow.clockwise")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(.secondary)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        Task { await store.refresh() }
                    }
                    .help("Refresh")
                    .opacity(store.loading ? 0.4 : 1)
            }

            if let reset = store.resetDate {
                HStack(spacing: 4) {
                    Text("Renews \(shortDate(reset))")
                    if let pace = store.pacePercent, let average = store.dailyAverage {
                        Text("·")
                        Text("On pace for \(Int(pace.rounded()))%")
                        Text("·")
                        Text(String(format: "%.1f%%/day", average))
                    }
                }
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
            }

            if let error = store.error {
                Text(error)
                    .font(.system(size: 10.5))
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var chart: some View {
        VStack(alignment: .leading, spacing: 6) {
            UsageChart(
                points: store.chartPoints,
                forecast: store.pacePercent,
                totalDays: 30,
                color: store.accent
            )

            HStack(spacing: 12) {
                if let start = store.cycleStart {
                    Text(shortDate(start).uppercased())
                }
                Spacer()
                HStack(spacing: 4) {
                    Circle()
                        .fill(store.accent)
                        .frame(width: 4, height: 4)
                    Text("Usage")
                }
                HStack(spacing: 4) {
                    Rectangle()
                        .fill(Color(red: 0.98, green: 0.75, blue: 0.25))
                        .frame(width: 9, height: 1.5)
                    Text("Forecast")
                }
                Spacer()
                if let reset = store.resetDate {
                    Text(shortDate(reset).uppercased())
                }
            }
            .font(.system(size: 9, weight: .medium))
            .foregroundStyle(.tertiary)
        }
    }

    private var limitsCard: some View {
        Card {
            Text("Limits")
                .font(.system(size: 12, weight: .semibold))
            if let go = store.limits?.go {
                LimitRow(name: "Rolling", window: go.rolling, color: store.accent)
                LimitRow(name: "Weekly", window: go.weekly, color: store.accent)
                LimitRow(name: "Monthly", window: go.monthly, color: store.accent)
            } else {
                Text("Waiting for usage data…")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var modelsCard: some View {
        Card {
            HStack {
                Text("Models")
                    .font(.system(size: 12, weight: .semibold))
                Spacer()
                if let stats = store.stats {
                    Text("\(stats.sessions) sessions · \(compactCount(stats.steps)) steps")
                        .font(.system(size: 10))
                        .foregroundStyle(.secondary)
                }
            }

            if let models = store.stats?.models, !models.isEmpty {
                let total = max(1, models.reduce(0) { $0 + $1.steps })
                ForEach(Array(models.prefix(3).enumerated()), id: \.offset) { _, usage in
                    let share = Double(usage.steps) / Double(total) * 100
                    VStack(alignment: .leading, spacing: 3) {
                        HStack {
                            Text(usage.model.id)
                                .font(.system(size: 11, weight: .medium))
                                .lineLimit(1)
                            Spacer()
                            Text("\(Int(share.rounded()))%")
                                .font(.system(size: 10.5))
                                .monospacedDigit()
                                .foregroundStyle(.secondary)
                        }
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Capsule().fill(Color.primary.opacity(0.10))
                                Capsule()
                                    .fill(Color.primary.opacity(0.45))
                                    .frame(width: max(2, geo.size.width * share / 100))
                            }
                        }
                        .frame(height: 3)
                    }
                }
            } else {
                Text("No model activity this cycle")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }

            if let stats = store.stats {
                Divider().opacity(0.5)
                HStack {
                    stat("Tokens", compactCount(stats.tokens.total))
                    Spacer()
                    stat("Cost", String(format: "$%.2f", stats.cost))
                    Spacer()
                    stat("Prompts", "\(stats.prompts)")
                }
            }
        }
    }

    private func stat(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(label)
                .font(.system(size: 9.5))
                .foregroundStyle(.secondary)
            Text(value)
                .font(.system(size: 12, weight: .medium))
                .monospacedDigit()
        }
    }

    private var footer: some View {
        HStack(spacing: 10) {
            HStack(spacing: 4) {
                Image(systemName: "arrow.up.right.square")
                Text("Open dashboard")
            }
            .font(.system(size: 11))
            .foregroundStyle(.secondary)
            .contentShape(Rectangle())
            .onTapGesture {
                if let string = store.limits?.workspace.usageURL,
                   let url = URL(string: string) {
                    NSWorkspace.shared.open(url)
                }
            }

            Spacer()

            if let updated = store.lastUpdated {
                Text("Updated \(updated.formatted(date: .omitted, time: .shortened))")
                    .font(.system(size: 9.5))
                    .foregroundStyle(.tertiary)
            }

            Text("Quit")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .contentShape(Rectangle())
                .onTapGesture {
                    NSApplication.shared.terminate(nil)
                }
        }
    }
}

// MARK: - App

@main
struct OpenCodeDashApp: App {
    @StateObject private var store = UsageStore.shared

    init() {
        Task { @MainActor in UsageStore.shared.start() }
    }

    var body: some Scene {
        MenuBarExtra {
            PopoverView(store: store)
        } label: {
            Label(store.percentLabel, systemImage: "chart.line.uptrend.xyaxis")
                .monospacedDigit()
        }
        .menuBarExtraStyle(.window)
    }
}
