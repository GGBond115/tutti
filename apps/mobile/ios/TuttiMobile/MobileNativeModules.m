#import <React/RCTBridgeModule.h>
#import <React/RCTEventEmitter.h>

@interface RCT_EXTERN_MODULE(TuttiAppLifecycle, RCTEventEmitter)

RCT_EXTERN__BLOCKING_SYNCHRONOUS_METHOD(isForeground)

@end

@interface RCT_EXTERN_MODULE(TuttiMobileSecurity, NSObject)

RCT_EXTERN_METHOD(getOrCreateIdentity
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(sign
                  : (NSString *)message resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(loadSession
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(saveSession
                  : (NSString *)sessionId userId
                  : (NSString *)userId email
                  : (NSString *)email name
                  : (NSString *)name avatarURL
                  : (NSString *)avatarURL resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(clearSession
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(clearLegacySessionCookie
                  : (NSString *)accountBaseURL resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(startBrowserLogin
                  : (NSString *)appId authLoginURL
                  : (NSString *)authLoginURL appCallbackURL
                  : (NSString *)appCallbackURL resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(scanQRCode
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(cancelQRCodeScan
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

@end

@interface RCT_EXTERN_MODULE(TuttiMobilePreferences, NSObject)

RCT_EXTERN__BLOCKING_SYNCHRONOUS_METHOD(loadThemePreference)

RCT_EXTERN_METHOD(saveThemePreference
                  : (NSString *)preference resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

@end
