import React
import UIKit

@objc(TuttiAppLifecycle)
final class AppLifecycleModule: RCTEventEmitter {
  private var observing = false

  @objc
  override static func requiresMainQueueSetup() -> Bool {
    true
  }

  override func supportedEvents() -> [String] {
    [Self.eventName]
  }

  @objc
  func isForeground() -> NSNumber {
    if Thread.isMainThread {
      return NSNumber(
        value: UIApplication.shared.applicationState != .background
      )
    }
    return DispatchQueue.main.sync {
      NSNumber(value: UIApplication.shared.applicationState != .background)
    }
  }

  override func startObserving() {
    guard !observing else {
      return
    }
    observing = true
    NotificationCenter.default.addObserver(
      self,
      selector: #selector(applicationDidBecomeActive),
      name: UIApplication.didBecomeActiveNotification,
      object: nil
    )
    NotificationCenter.default.addObserver(
      self,
      selector: #selector(applicationDidEnterBackground),
      name: UIApplication.didEnterBackgroundNotification,
      object: nil
    )
  }

  override func stopObserving() {
    observing = false
    NotificationCenter.default.removeObserver(self)
  }

  deinit {
    NotificationCenter.default.removeObserver(self)
  }

  @objc
  private func applicationDidBecomeActive() {
    sendEvent(withName: Self.eventName, body: true)
  }

  @objc
  private func applicationDidEnterBackground() {
    sendEvent(withName: Self.eventName, body: false)
  }

  private static let eventName = "TuttiAppLifecycleChanged"
}
