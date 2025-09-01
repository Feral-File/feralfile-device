use bluer::Result;
use bluer::gatt::local::CharacteristicNotifier;

#[cfg_attr(test, mockall::automock)]
pub trait Notifier {
    async fn notify(&mut self, payload: Vec<u8>) -> Result<()>;
}

impl Notifier for CharacteristicNotifier {
    async fn notify(&mut self, payload: Vec<u8>) -> Result<()> {
        CharacteristicNotifier::notify(self, payload).await
    }
}
