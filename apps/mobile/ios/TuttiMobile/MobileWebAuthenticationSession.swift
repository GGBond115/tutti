import AuthenticationServices
import Foundation
import UIKit

enum MobileWebAuthenticationResult {
  case callback
  case cancelled
  case failed
}

final class MobileWebAuthenticationSession: NSObject,
  ASWebAuthenticationPresentationContextProviding
{
  private var authenticationSession: ASWebAuthenticationSession?
  private weak var presentationAnchor: ASPresentationAnchor?

  func start(
    url: URL,
    callbackScheme: String,
    completion: @escaping (MobileWebAuthenticationResult) -> Void
  ) -> Bool {
    guard authenticationSession == nil, let anchor = Self.currentAnchor() else {
      return false
    }

    let completionHandler: ASWebAuthenticationSession.CompletionHandler = {
      [weak self] _, error in
      self?.authenticationSession = nil
      self?.presentationAnchor = nil
      guard let error else {
        completion(.callback)
        return
      }
      let browserError = error as NSError
      let cancelled =
        browserError.domain == ASWebAuthenticationSessionError.errorDomain
        && browserError.code
          == ASWebAuthenticationSessionError.Code.canceledLogin.rawValue
      completion(cancelled ? .cancelled : .failed)
    }
    let session: ASWebAuthenticationSession
    if #available(iOS 17.4, *) {
      session = ASWebAuthenticationSession(
        url: url,
        callback: .customScheme(callbackScheme),
        completionHandler: completionHandler
      )
    } else {
      session = ASWebAuthenticationSession(
        url: url,
        callbackURLScheme: callbackScheme,
        completionHandler: completionHandler
      )
    }
    presentationAnchor = anchor
    authenticationSession = session
    session.presentationContextProvider = self
    session.prefersEphemeralWebBrowserSession = false
    guard session.start() else {
      authenticationSession = nil
      presentationAnchor = nil
      return false
    }
    return true
  }

  func cancel() {
    authenticationSession?.cancel()
    authenticationSession = nil
    presentationAnchor = nil
  }

  func presentationAnchor(
    for _: ASWebAuthenticationSession
  ) -> ASPresentationAnchor {
    presentationAnchor ?? ASPresentationAnchor()
  }

  private static func currentAnchor() -> ASPresentationAnchor? {
    let scenes = UIApplication.shared.connectedScenes.compactMap {
      $0 as? UIWindowScene
    }
    return scenes
      .flatMap(\.windows)
      .first(where: \.isKeyWindow)
      ?? scenes.flatMap(\.windows).first
  }
}
