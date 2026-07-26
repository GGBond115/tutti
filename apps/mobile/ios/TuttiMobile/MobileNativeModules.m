#import <React/RCTBridgeModule.h>

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
                  : (NSString *)name resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(clearSession
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(installSessionCookie
                  : (NSString *)accountBaseURL sessionId
                  : (NSString *)sessionId resolver
                  : (RCTPromiseResolveBlock)resolve rejecter
                  : (RCTPromiseRejectBlock)reject)

RCT_EXTERN_METHOD(clearSessionCookie
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

@end
