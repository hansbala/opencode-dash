import Foundation

// MARK: - Service models

struct ServiceInfo: Decodable {
    let url: String
    let password: String
}

struct LimitsEnvelope: Decodable {
    let data: LimitsInfo
}

struct LimitsInfo: Decodable {
    let workspace: Workspace
    let go: GoInfo?
}

struct Workspace: Decodable {
    let name: String?
    let usageURL: String
}

struct GoInfo: Decodable {
    let rolling: LimitWindow
    let weekly: LimitWindow
    let monthly: LimitWindow
}

struct LimitWindow: Decodable {
    let percent: Double
    let resetsAt: Double

    var resetDate: Date { Date(timeIntervalSince1970: resetsAt / 1000) }
}

struct StatsEnvelope: Decodable {
    let data: StatsInfo
}

struct StatsInfo: Decodable {
    let sessions: Int
    let prompts: Int
    let steps: Int
    let tokens: TokenSummary
    let cost: Double
    let activity: [DayActivity]
    let models: [ModelUsage]
}

struct TokenSummary: Decodable {
    let input: Int
    let output: Int
    let reasoning: Int
    let cache: CacheTokens

    var total: Int { input + output + reasoning + cache.read + cache.write }
}

struct CacheTokens: Decodable {
    let read: Int
    let write: Int
}

struct DayActivity: Decodable {
    let date: String
    let steps: Int
}

struct ModelUsage: Decodable {
    let model: ModelRef
    let steps: Int
    let cost: Double
}

struct ModelRef: Decodable {
    let id: String
}

struct SessionSummary: Decodable {
    let id: String
    let title: String?
    let model: ModelRef?
    let cost: Double
    let time: SessionTime
}

struct SessionTime: Decodable {
    let created: Double
}

struct SessionList: Decodable {
    let data: [SessionSummary]
}

// MARK: - Snapshot

struct ChartPoint {
    let day: Double
    let percent: Double
}

struct UsageSnapshot {
    let limits: LimitsInfo
    let stats: StatsInfo
    let fetchedAt: Date

    var monthly: LimitWindow? { limits.go?.monthly }
    var percent: Double { monthly?.percent ?? 0 }
    var resetDate: Date? { monthly?.resetDate }
    var cycleStart: Date? { resetDate?.addingTimeInterval(-30 * 86_400) }

    /// Linear projection of this cycle's usage at the renewal date.
    var pacePercent: Double? {
        guard let resetDate, let start = cycleStart else { return nil }
        let total = resetDate.timeIntervalSince(start)
        let elapsed = Date().timeIntervalSince(start)
        guard elapsed > 1, total > 1 else { return nil }
        return percent * total / elapsed
    }

    var dailyAverage: Double? {
        guard let start = cycleStart else { return nil }
        let elapsed = Date().timeIntervalSince(start) / 86_400
        guard elapsed > 0.2 else { return nil }
        return percent / elapsed
    }

    var modelsByUsage: [ModelUsage] {
        stats.models.sorted { $0.steps > $1.steps }
    }

    func share(of usage: ModelUsage) -> Double {
        let total = max(1, stats.models.reduce(0) { $0 + $1.steps })
        return Double(usage.steps) / Double(total) * 100
    }

    var chartPoints: [ChartPoint] {
        guard let start = cycleStart else { return [] }
        let calendar = Calendar.current
        let startDay = calendar.startOfDay(for: start)
        let totalSteps = max(1, stats.activity.reduce(0) { $0 + $1.steps })

        var cumulative = 0.0
        var points: [ChartPoint] = [.init(day: 0, percent: 0)]

        let parser = DateFormatter()
        parser.locale = Locale(identifier: "en_US_POSIX")
        parser.dateFormat = "yyyy-MM-dd"

        for day in stats.activity {
            guard let date = parser.date(from: day.date) else { continue }
            let index = calendar.dateComponents([.day], from: startDay, to: date).day ?? 0
            cumulative += Double(day.steps)
            points.append(.init(day: Double(index) + 1, percent: percent * cumulative / Double(totalSteps)))
        }

        let today = calendar.dateComponents([.day], from: startDay, to: Date()).day ?? 0
        if points.last?.day ?? 0 < Double(today) {
            points.append(.init(day: Double(today), percent: percent))
        }
        return points
    }
}

// MARK: - Client

enum OpenCodeError: LocalizedError {
    case serviceUnavailable
    case badResponse
    case http(Int)

    var errorDescription: String? {
        switch self {
        case .serviceUnavailable:
            return "OpenCode service not found. Start OpenCode or run `opencode serve --service`."
        case .badResponse:
            return "The OpenCode service returned an unexpected response."
        case .http(let code):
            return "The OpenCode service returned HTTP \(code)."
        }
    }
}

struct OpenCodeClient {
    var serviceFile: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".local/state/opencode/service.json")
    }

    func snapshot() async throws -> UsageSnapshot {
        guard let data = try? Data(contentsOf: serviceFile),
              let service = try? JSONDecoder().decode(ServiceInfo.self, from: data),
              let base = URL(string: service.url) else {
            throw OpenCodeError.serviceUnavailable
        }

        let limits: LimitsEnvelope = try await get(
            base: base,
            password: service.password,
            path: "/api/experimental/usage/limits"
        )

        let reset = limits.data.go?.monthly.resetDate ?? Date().addingTimeInterval(30 * 86_400)
        let start = reset.addingTimeInterval(-30 * 86_400)
        let stats: StatsEnvelope = try await get(
            base: base,
            password: service.password,
            path: "/api/experimental/session/stats",
            query: [
                URLQueryItem(name: "from", value: String(Int(start.timeIntervalSince1970 * 1000))),
                URLQueryItem(name: "to", value: String(Int(Date().timeIntervalSince1970 * 1000))),
                URLQueryItem(name: "tools", value: "none"),
            ]
        )

        return UsageSnapshot(limits: limits.data, stats: stats.data, fetchedAt: Date())
    }

    func recentSessions(limit: Int = 10) async throws -> [SessionSummary] {
        guard let data = try? Data(contentsOf: serviceFile),
              let service = try? JSONDecoder().decode(ServiceInfo.self, from: data),
              let base = URL(string: service.url) else {
            throw OpenCodeError.serviceUnavailable
        }

        let list: SessionList = try await get(
            base: base,
            password: service.password,
            path: "/api/session",
            query: [URLQueryItem(name: "limit", value: String(limit))]
        )
        return Array(list.data.prefix(limit))
    }

    private func get<T: Decodable>(
        base: URL,
        password: String,
        path: String,
        query: [URLQueryItem] = []
    ) async throws -> T {
        guard var components = URLComponents(url: base, resolvingAgainstBaseURL: false) else {
            throw OpenCodeError.badResponse
        }
        components.path = path
        components.queryItems = query.isEmpty ? nil : query
        guard let url = components.url else { throw OpenCodeError.badResponse }

        var request = URLRequest(url: url)
        request.timeoutInterval = 8
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let credentials = Data("opencode:\(password)".utf8).base64EncodedString()
        request.setValue("Basic \(credentials)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw OpenCodeError.badResponse }
        guard http.statusCode == 200 else { throw OpenCodeError.http(http.statusCode) }
        return try JSONDecoder().decode(T.self, from: data)
    }
}

// MARK: - Formatting

func compactCount(_ value: Int) -> String {
    let number = Double(value)
    switch number {
    case 1_000_000_000...:
        return String(format: "%.2fB", number / 1_000_000_000)
    case 1_000_000...:
        return String(format: "%.1fM", number / 1_000_000)
    case 1_000...:
        return String(format: "%.0fK", number / 1_000)
    default:
        return "\(value)"
    }
}

func countdown(to date: Date) -> String {
    let seconds = max(0, date.timeIntervalSinceNow)
    if seconds < 60 { return "now" }
    let days = Int(seconds / 86_400)
    let hours = Int(seconds.truncatingRemainder(dividingBy: 86_400) / 3_600)
    let minutes = Int(seconds.truncatingRemainder(dividingBy: 3_600) / 60)
    if days > 0 { return "\(days)d \(hours)h" }
    if hours > 0 { return "\(hours)h \(minutes)m" }
    return "\(minutes)m"
}

func shortDate(_ date: Date) -> String {
    date.formatted(.dateTime.month(.abbreviated).day())
}

func relativeTime(_ date: Date) -> String {
    let seconds = max(0, Date().timeIntervalSince(date))
    if seconds < 60 { return "now" }
    if seconds < 3_600 { return "\(Int(seconds / 60))m ago" }
    if seconds < 86_400 { return "\(Int(seconds / 3_600))h ago" }
    return "\(Int(seconds / 86_400))d ago"
}
